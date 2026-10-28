package usecase_test

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
)

// printRecord is one row of the fake print log.
type printRecord struct {
	Type       printmodel.DocumentType
	DocumentID string
	Paper      printmodel.Paper
	Version    int
	Copy       bool
}

// fakePrints is an in-memory usecase.PrintStore.
type fakePrints struct {
	settings map[printmodel.DocumentType]usecase.PrintSetting
	log      []printRecord
	err      error
	// stateErr and recordErr fail only State and Record.
	stateErr, recordErr error
}

func (f *fakePrints) Settings(context.Context, string) (map[printmodel.DocumentType]usecase.PrintSetting, error) {
	return f.settings, f.err
}

func (f *fakePrints) SaveSetting(_ context.Context, _ string, t printmodel.DocumentType, s usecase.PrintSetting) error {
	if f.err != nil {
		return f.err
	}

	if f.settings == nil {
		f.settings = map[printmodel.DocumentType]usecase.PrintSetting{}
	}

	f.settings[t] = s

	return nil
}

func (f *fakePrints) State(
	_ context.Context, _ string, t printmodel.DocumentType, documentID string, paper printmodel.Paper,
) (usecase.PrintState, error) {
	if f.err != nil || f.stateErr != nil {
		return usecase.PrintState{}, errors.Join(f.err, f.stateErr)
	}

	var state usecase.PrintState

	for _, r := range f.log {
		if r.Type != t || r.DocumentID != documentID {
			continue
		}

		state.Prints++

		if r.Paper == paper && state.FirstVersion == 0 {
			state.FirstVersion = r.Version
		}
	}

	return state, nil
}

func (f *fakePrints) Record(
	_ context.Context, _ string, t printmodel.DocumentType, documentID string, paper printmodel.Paper, version int, copyMark bool,
) error {
	if f.err != nil || f.recordErr != nil {
		return errors.Join(f.err, f.recordErr)
	}

	f.log = append(f.log, printRecord{Type: t, DocumentID: documentID, Paper: paper, Version: version, Copy: copyMark})

	return nil
}
