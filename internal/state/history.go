package state

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/diagnostics"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/hardware"
)

const MaxHistory = 100

const historyVersion = 1
const historyPrefix = "event-"
const stagePrefix = ".stage-event-"
const historyStamp = "20060102T150405.000000000Z"

var errAlreadyRestored = errors.New("tindakan sudah dipulihkan")

type Entry struct {
	Version  int            `json:"version"`
	At       time.Time      `json:"at"`
	Kind     string         `json:"kind"`
	Category string         `json:"category"`
	Findings *FindingCounts `json:"findings,omitempty"`
	PlanID   string         `json:"plan_id,omitempty"`
	Restores string         `json:"restores,omitempty"`
	Action   *ActionSummary `json:"action,omitempty"`

	dir string
}

type FindingCounts struct {
	Total       int `json:"total"`
	Info        int `json:"info"`
	Warning     int `json:"warning"`
	Critical    int `json:"critical"`
	Unavailable int `json:"unavailable"`
}

type ActionSummary struct {
	Status                actions.Status `json:"status"`
	Started               bool           `json:"started"`
	CancellationRequested bool           `json:"cancellation_requested"`
	Changed               *bool          `json:"changed,omitempty"`
	PotentialDiscardBytes *uint64        `json:"potential_discard_bytes,omitempty"`
	RemovedBytes          *uint64        `json:"removed_bytes,omitempty"`
	FreeSpaceDelta        *int64         `json:"free_space_delta,omitempty"`
	VerifyAfterRestart    bool           `json:"verify_after_restart,omitempty"`
	CanRestore            bool           `json:"can_restore"`
}

func (s Store) RecordScan(category string, at time.Time, findings []diagnostics.Finding) error {
	if !scanCategory(category) {
		return errors.New("unknown scan category")
	}
	counts := FindingCounts{Total: len(findings)}
	for _, finding := range findings {
		switch finding.Severity {
		case diagnostics.Warning:
			counts.Warning++
		case diagnostics.Critical:
			counts.Critical++
		default:
			counts.Info++
		}
		if finding.Availability != domain.Available && finding.Availability != domain.Partial {
			counts.Unavailable++
		}
	}
	return s.appendEvent(Entry{At: at, Kind: "scan", Category: category, Findings: &counts}, nil)
}

func (s Store) RecordAction(result actions.Result) error {
	if !actionCategory(result.Plan.Kind) || !actionStatus(result.Status) {
		return errors.New("unknown maintenance action or status")
	}
	summary := &ActionSummary{
		Status: result.Status, Started: result.Started, CancellationRequested: result.CancellationRequested,
		Changed: result.Changed, PotentialDiscardBytes: result.PotentialDiscardBytes,
		RemovedBytes: result.RemovedBytes, FreeSpaceDelta: result.FreeSpaceDelta,
	}
	entry := Entry{At: result.FinishedAt, Kind: "action", Category: result.Plan.Kind, Action: summary}
	var receipt []byte
	if validPlanID(result.Plan.ID) {
		entry.PlanID = result.Plan.ID
		summary.VerifyAfterRestart = result.Started && result.Plan.Kind == actions.FirmwareUpdate && (result.Status == actions.Partial || result.Status == actions.Unknown)
		isRestore := strings.HasPrefix(result.Plan.Kind, "restore-")
		if isRestore && result.Status == actions.Verified && result.Started &&
			validPlanID(result.Plan.Restores) && result.Plan.Restores != result.Plan.ID {
			entry.Restores = result.Plan.Restores
		}
		summary.CanRestore = !isRestore && result.Status == actions.Verified && result.Receipt != ""
		if summary.CanRestore || summary.VerifyAfterRestart {
			var err error
			receipt, err = json.Marshal(result)
			if err != nil {
				return err
			}
			if len(receipt) > 64<<10 {
				return errors.New("maintenance receipt exceeds size limit")
			}
		}
	}
	return s.appendEvent(entry, receipt)
}

func (s Store) appendEvent(entry Entry, receipt []byte) error {
	if entry.At.IsZero() {
		return errors.New("history requires a captured timestamp")
	}
	entry.Version, entry.At = historyVersion, entry.At.UTC()
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	root, err := privateRoot(s.StateDir, true)
	if err != nil {
		return err
	}
	defer root.Close()
	token := rand.Text()
	stage := stagePrefix + token
	if err := root.Mkdir(stage, 0o700); err != nil {
		return err
	}
	defer removeEvent(root, stage)
	child, err := openEvent(root, stage)
	if err != nil {
		return err
	}
	if err := atomicWrite(child, "entry.json", data, false); err != nil {
		child.Close()
		return err
	}
	if len(receipt) > 0 {
		if err := atomicWrite(child, "receipt.json", receipt, false); err != nil {
			child.Close()
			return err
		}
	}
	if err := child.Close(); err != nil {
		return err
	}
	name := historyPrefix + entry.At.Format(historyStamp) + "-" + token
	if err := root.Rename(stage, name); err != nil {
		return err
	}
	files, err := eventDirectories(root)
	if err != nil {
		return err
	}
	for _, name := range files[min(len(files), MaxHistory):] {
		if err := removeEvent(root, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return pruneStages(root, time.Now().Add(-24*time.Hour))
}

func (s Store) History() ([]Entry, error) {
	root, err := privateRoot(s.StateDir, false)
	if errors.Is(err, fs.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readHistory(root)
}

type FirmwareHandoff struct {
	Target    string
	CreatedAt time.Time
	entry     Entry
}

func (s Store) FirmwareHandoff(planID string) (FirmwareHandoff, error) {
	if !validPlanID(planID) {
		return FirmwareHandoff{}, errors.New("invalid plan ID")
	}
	root, err := privateRoot(s.StateDir, false)
	if err != nil {
		return FirmwareHandoff{}, err
	}
	defer root.Close()
	entries, historyErr := readHistory(root)
	for _, entry := range entries {
		if entry.PlanID != planID || entry.Category != actions.FirmwareUpdate || entry.Action == nil || !entry.Action.Started ||
			(entry.Action.Status != actions.Partial && entry.Action.Status != actions.Unknown) {
			continue
		}
		event, err := openEvent(root, entry.dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return FirmwareHandoff{}, err
		}
		data, err := readPrivate(event, "receipt.json", 64<<10)
		event.Close()
		if err != nil {
			return FirmwareHandoff{}, err
		}
		var result actions.Result
		if err = decodeEvent(data, &result); err != nil || result.Plan.ID != planID || result.Plan.Kind != actions.FirmwareUpdate ||
			!result.Started || (result.Status != actions.Partial && result.Status != actions.Unknown) ||
			result.Plan.CreatedAt.IsZero() || !result.FinishedAt.Equal(entry.At) {
			return FirmwareHandoff{}, errors.New("invalid firmware handoff")
		}
		if _, _, err := firmware.SplitTarget(result.Plan.Target); err != nil {
			return FirmwareHandoff{}, err
		}
		return FirmwareHandoff{Target: result.Plan.Target, CreatedAt: result.Plan.CreatedAt, entry: entry}, nil
	}
	return FirmwareHandoff{}, errors.Join(fs.ErrNotExist, historyErr)
}

// RecordFirmwareVerification links the result to its original action while the caller holds the action lock.
func (s Store) RecordFirmwareVerification(handoff FirmwareHandoff, result firmware.Verification, at time.Time) error {
	entry := handoff.entry
	if entry.Action == nil || result.Target != handoff.Target {
		return errors.New("invalid firmware verification target")
	}
	if !entry.Action.VerifyAfterRestart {
		return nil
	}
	summary := *entry.Action
	switch {
	case result.State == "verified" && result.Availability == domain.Available:
		_, version, _ := firmware.SplitTarget(handoff.Target)
		if result.Installed != version {
			return errors.New("invalid firmware verification version")
		}
		summary.Status, summary.Changed = actions.Verified, new(true)
	case result.State == "failed" && result.Availability == domain.Error:
		summary.Status = actions.Failed
	default:
		return nil
	}
	summary.VerifyAfterRestart = false
	entry.Action, entry.At = &summary, at
	return s.appendEvent(entry, nil)
}

func (s Store) RollbackReceipt(planID string) (actions.Result, error) {
	if !validPlanID(planID) {
		return actions.Result{}, errors.New("invalid plan ID")
	}
	root, err := privateRoot(s.StateDir, false)
	if err != nil {
		return actions.Result{}, err
	}
	defer root.Close()
	entries, historyErr := readHistory(root)
	if slices.ContainsFunc(entries, func(entry Entry) bool { return entry.Restores == planID }) {
		return actions.Result{}, errAlreadyRestored
	}
	for _, entry := range entries {
		if entry.PlanID != planID || entry.Action == nil || !entry.Action.CanRestore {
			continue
		}
		event, err := openEvent(root, entry.dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return actions.Result{}, err
		}
		data, err := readPrivate(event, "receipt.json", 64<<10)
		event.Close()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return actions.Result{}, err
		}
		var result actions.Result
		if err := decodeEvent(data, &result); err != nil || result.Plan.ID != planID || result.Receipt == "" ||
			result.Plan.Kind != entry.Category || result.Status != actions.Verified {
			return actions.Result{}, errors.New("invalid local rollback receipt")
		}
		return result, nil
	}
	return actions.Result{}, errors.Join(fs.ErrNotExist, historyErr)
}

func readHistory(root *os.Root) ([]Entry, error) {
	dirs, err := eventDirectories(root)
	if err != nil {
		return nil, err
	}
	dirs = dirs[:min(len(dirs), MaxHistory)]
	out := make([]Entry, 0, len(dirs))
	restored := make(map[string]bool)
	finishedFirmware := make(map[string]bool)
	var errs []error
	for _, name := range dirs {
		entry, err := readEntry(root, name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, entry)
		if entry.Restores != "" {
			restored[entry.Restores] = true
		}
		if entry.Category == actions.FirmwareUpdate && entry.PlanID != "" && entry.Action != nil && entry.Action.Started &&
			(entry.Action.Status == actions.Verified || entry.Action.Status == actions.Failed) {
			finishedFirmware[entry.PlanID] = true
		}
	}
	for _, entry := range out {
		if entry.Action != nil && restored[entry.PlanID] {
			entry.Action.CanRestore = false
		}
		if entry.Action != nil && finishedFirmware[entry.PlanID] {
			entry.Action.VerifyAfterRestart = false
		}
	}
	return out, errors.Join(errs...)
}

func readEntry(root *os.Root, name string) (Entry, error) {
	event, err := openEvent(root, name)
	if err != nil {
		return Entry{}, err
	}
	defer event.Close()
	data, err := readPrivate(event, "entry.json", 16<<10)
	if err != nil {
		return Entry{}, err
	}
	var entry Entry
	if err := decodeEvent(data, &entry); err != nil || !validEntry(entry) {
		return Entry{}, errors.New("invalid local history entry")
	}
	entry.dir = name
	return entry, nil
}

func decodeEvent(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("extra data in local history")
	}
	return nil
}

func validEntry(entry Entry) bool {
	if entry.Version != historyVersion || entry.At.IsZero() {
		return false
	}
	switch entry.Kind {
	case "scan":
		return scanCategory(entry.Category) && entry.Findings != nil && entry.Action == nil && entry.PlanID == "" && entry.Restores == ""
	case "action":
		return actionCategory(entry.Category) && entry.Action != nil && actionStatus(entry.Action.Status) && entry.Findings == nil &&
			(!entry.Action.VerifyAfterRestart || (entry.Category == actions.FirmwareUpdate && validPlanID(entry.PlanID))) &&
			((entry.PlanID == "" && !entry.Action.CanRestore) || validPlanID(entry.PlanID)) &&
			(entry.Restores == "" || (strings.HasPrefix(entry.Category, "restore-") && entry.Action.Status == actions.Verified &&
				entry.Action.Started && !entry.Action.CanRestore && validPlanID(entry.PlanID) &&
				validPlanID(entry.Restores) && entry.Restores != entry.PlanID))
	default:
		return false
	}
}

func scanCategory(category string) bool {
	return slices.Contains(assistant.Menu, assistant.Intent(category)) || category == "check" || category == "report" || category == "doctor"
}

func actionCategory(category string) bool {
	switch category {
	case actions.PowerProfile, actions.TrimMount, actions.EnableTrimTimer, actions.CleanAPT, actions.CleanPacman, actions.CleanDNF, actions.RestorePower, actions.RestoreTrimTimer, actions.FirmwareUpdate, actions.FirmwareRefresh, actions.FilesystemRepair,
		hardware.PowerLimit, hardware.CoreMax, hardware.VoltageOffset, hardware.RestorePowerLimit, hardware.RestoreCoreMax, hardware.RestoreVoltageOffset:
		return true
	}
	return false
}

func actionStatus(status actions.Status) bool {
	switch status {
	case actions.NotStarted, actions.Verified, actions.Failed, actions.Partial, actions.Unknown:
		return true
	}
	return false
}

func eventDirectories(root *os.Root) ([]string, error) {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	var out []string
	for i := len(entries) - 1; i >= 0; i-- {
		name := entries[i].Name()
		stamp, found := strings.CutPrefix(name, historyPrefix)
		if !found || len(stamp) != len(historyStamp)+1+26 || stamp[len(historyStamp)] != '-' ||
			!validEventToken(stamp[len(historyStamp)+1:]) {
			continue
		}
		if _, err := time.Parse(historyStamp, stamp[:len(historyStamp)]); err == nil {
			out = append(out, name)
		}
	}
	return out, nil
}

func validEventToken(token string) bool {
	return len(token) == 26 && strings.Trim(token, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") == ""
}

func openEvent(root *os.Root, name string) (*os.Root, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || !private(before) {
		return nil, errors.New("history entry directory is not private")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err != nil {
		child.Close()
		return nil, err
	}
	if !os.SameFile(before, after) {
		child.Close()
		return nil, errors.New("history entry directory changed")
	}
	return child, nil
}

func removeEvent(root *os.Root, name string) error {
	child, err := openEvent(root, name)
	if err != nil {
		return err
	}
	files := []string{"entry.json", "receipt.json"}
	if strings.HasPrefix(name, stagePrefix) {
		entries, err := fs.ReadDir(child.FS(), ".")
		if err != nil {
			child.Close()
			return err
		}
		for _, entry := range entries {
			token, found := strings.CutPrefix(entry.Name(), ".tmp-")
			if found && validEventToken(token) {
				files = append(files, entry.Name())
			}
		}
	}
	for _, file := range files {
		if err := child.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			child.Close()
			return err
		}
	}
	if err := child.Close(); err != nil {
		return err
	}
	return root.Remove(name)
}

func pruneStages(root *os.Root, cutoff time.Time) error {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		token, found := strings.CutPrefix(entry.Name(), stagePrefix)
		if !found || !validEventToken(token) || !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			if err := removeEvent(root, entry.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}
