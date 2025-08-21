package filemock

import (
	"io/fs"

	"github.com/stretchr/testify/mock"
)

type Writer struct {
	mock.Mock
}

func (m *Writer) Write(filepath string, data []byte, flag int, perm fs.FileMode) (int, error) {
	args := m.Called(filepath, data, flag, perm)
	return args.Int(0), args.Error(1)
}
