package platform

import (
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

var topIdle = regexp.MustCompile(`^CPU usage:[^\r\n]*, ([0-9]+(?:\.[0-9]+)?)% idle`)

func darwinCPUSamples(out string) []float64 {
	var samples []float64
	seen := false
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.HasPrefix(line, "CPU usage:") {
			continue
		}
		// top has no preceding interval for its first logged sample.
		if !seen {
			seen = true
			continue
		}
		match := topIdle.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if idle, err := strconv.ParseFloat(match[1], 64); err == nil && goodPercent(idle) {
			samples = append(samples, 100-idle)
		}
	}
	return samples
}

var vmPageSize = regexp.MustCompile(`page size of ([0-9]+) bytes`)

func darwinFreeMemory(out string) (uint64, bool) {
	match := vmPageSize.FindStringSubmatch(out)
	if match == nil {
		return 0, false
	}
	size, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil || size < 4096 || size > 65536 || size&(size-1) != 0 {
		return 0, false
	}
	for line := range strings.SplitSeq(out, "\n") {
		value, found := strings.CutPrefix(line, "Pages free:")
		if found {
			pages, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(value), "."), 10, 64)
			return pages * size, err == nil && pages <= math.MaxUint64/size
		}
	}
	return 0, false
}

func darwinPS(out string, cpus int) []domain.Process {
	if cpus <= 0 {
		return nil
	}
	var processes []domain.Process
	for line := range strings.SplitSeq(out, "\n") {
		cpuText, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		rssText, name, _ := strings.Cut(strings.TrimSpace(rest), " ")
		cpu, cpuErr := strconv.ParseFloat(cpuText, 64)
		rss, rssErr := strconv.ParseUint(rssText, 10, 64)
		name = domain.SafeText(path.Base(strings.TrimSpace(name)))
		if name == "." || name == "" || cpuErr != nil || rssErr != nil || rss > math.MaxUint64/1024 || !goodPercent(cpu/float64(cpus)) {
			continue
		}
		processes = append(processes, domain.Process{Name: name, CPU: cpu / float64(cpus), RSS: rss * 1024})
	}
	return topNativeProcesses(processes)
}

var pmsetPercent = regexp.MustCompile(`\s([0-9]{1,3})%;`)

func darwinPower(out string) (ac *bool, percent *float64, status domain.Availability) {
	status = domain.Unknown
	for line := range strings.SplitSeq(out, "\n") {
		switch strings.TrimSpace(line) {
		case "Now drawing from 'AC Power'":
			value := true
			ac = &value
		case "Now drawing from 'Battery Power'":
			value := false
			ac = &value
		}
		if strings.HasPrefix(strings.TrimSpace(line), "-InternalBattery") {
			if match := pmsetPercent.FindStringSubmatch(line); match != nil {
				if value, err := strconv.ParseFloat(match[1], 64); err == nil && goodPercent(value) {
					percent = &value
				}
			}
		}
	}
	if ac != nil || percent != nil {
		status = domain.Available
	}
	return
}
