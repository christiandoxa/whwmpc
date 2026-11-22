//go:build !linux

package actions

import (
	"context"
	"errors"
	"io"

	"github.com/christiandoxa/whwmpc/internal/domain"
)

type unsupported struct{}

func newPlatform() platform { return unsupported{} }

var errUnsupported = errors.New("tindakan ini tersedia di Linux dengan backend yang didukung")

func (unsupported) prepare(context.Context, string, string) (prepared, error) {
	return prepared{}, errUnsupported
}

func (unsupported) cleanup(context.Context) (Cleanup, error) {
	return Cleanup{Availability: domain.Unsupported, Category: "APT", Detail: errUnsupported.Error()}, nil
}

func (unsupported) trimTargets(context.Context) ([]TrimTarget, error) {
	return nil, errUnsupported
}

func (unsupported) authenticate(context.Context, io.Reader, io.Writer, io.Writer) error {
	return errUnsupported
}
