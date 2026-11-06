package usecase_test

import (
	"testing"

	"github.com/viettechno/vietgara-backend/internal/mocks"
)

type mocksMailer = mocks.MockOrderMailer

func newMailer(t *testing.T) *mocksMailer { return mocks.NewMockOrderMailer(t) }
