package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/power"
)

func (p linuxPlatform) profile(ctx context.Context, kind, target string) (prepared, error) {
	if !power.ValidProfile(target) {
		return prepared{}, errors.New("profil harus berupa balanced, power-saver, atau performance")
	}
	before := power.InspectProfiles(ctx, os.DirFS("/"), p.runner)
	if !before.CanChange() {
		detail := before.Detail
		if len(before.Conflicts) != 0 {
			detail = "Pengelola daya lain terdeteksi: " + strings.Join(before.Conflicts, ", ")
		}
		if detail == "" {
			detail = "Backend atau akses pengaturan belum dapat diverifikasi"
		}
		return prepared{}, fmt.Errorf("%s. Periksa dengan powerprofilesctl list di sesi desktop, lalu gunakan satu pengelola daya", safeText(detail))
	}
	if target == before.Active {
		return prepared{}, errors.New("profil ini sudah aktif")
	}
	args, err := power.ProfileCommand(before, target)
	if err != nil {
		return prepared{}, errors.New("profil ini tidak tersedia atau sedang dibatasi oleh power-profiles-daemon")
	}
	inv, err := newInvocation("busctl", args, false)
	if err != nil {
		return prepared{}, err
	}
	boot, err := bootIdentity()
	if err != nil {
		return prepared{}, err
	}
	profileIdentity := func(state power.Profiles) string { return digest(boot, state.Target, state.API, state, inv.identity()) }
	impact := map[string]string{
		"balanced":    "Menerapkan penyeimbangan konsumsi daya dan respons sistem.",
		"power-saver": "Mengutamakan konsumsi daya yang lebih rendah. Beban berat dapat berjalan lebih lambat.",
		"performance": "Mengutamakan respons saat beban tinggi. Konsumsi daya, panas, dan kebisingan dapat meningkat.",
	}[target]
	return prepared{
		plan: Plan{
			Kind: kind, Target: target, Initial: before.Active, Identity: profileIdentity(before),
			Operation: inv.spec, Privilege: inv.privilege, Impact: impact,
			Risk:     "Kebijakan Polkit pada sesi desktop menentukan izin. Pergantian profil dapat memengaruhi daya dan suhu.",
			Rollback: kind == PowerProfile,
		},
		run: func(ctx context.Context) attempt { return inv.run(ctx, 20*time.Second) },
		verify: func(ctx context.Context, step attempt) Result {
			current := power.InspectProfiles(ctx, os.DirFS("/"), p.runner)
			return verifyProfile(before, current, target, profileIdentity(current), step)
		},
	}, nil
}

func verifyProfile(before, after power.Profiles, target, id string, step attempt) Result {
	if after.Status != domain.Available || after.Target != before.Target || after.API != before.API || !power.ValidProfile(after.Active) {
		return Result{Status: Unknown, Message: withStepError("Keadaan profil pada daemon yang sama belum dapat dipastikan", step)}
	}
	changed := before.Active != after.Active
	message := "Profil yang terbaca kembali adalah " + after.Active
	switch {
	case after.Active == target && step.err == nil:
		return Result{Status: Verified, Message: message, Changed: boolPointer(changed), AfterIdentity: id}
	case changed:
		return Result{Status: Partial, Message: withStepError(message, step), Changed: boolPointer(changed)}
	case step.stopped:
		return Result{Status: Unknown, Message: withStepError(message, step)}
	default:
		return Result{Status: Failed, Message: withStepError(message, step), Changed: boolPointer(false)}
	}
}
