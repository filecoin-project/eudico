package mir

import (
	"fmt"
	"os"
	"path"

	"github.com/filecoin-project/mir/pkg/simplewal"
)

func NewWAL(ownID, walDir string) (*simplewal.WAL, error) {
	walPath := path.Join(walDir, fmt.Sprintf("%v", ownID))
	wal, err := simplewal.Open(walPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(walPath, 0700); err != nil {
		return nil, err
	}
	return wal, nil
}
