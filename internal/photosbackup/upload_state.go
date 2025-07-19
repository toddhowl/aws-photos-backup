package photosbackup

import (
	"encoding/json"
	"os"
)

type UploadState struct {
	CompletedMonths map[string]string `json:"completed_months"` // map[year-month]zipName
}

func LoadUploadState(path string) (*UploadState, error) {
	state := &UploadState{CompletedMonths: make(map[string]string)}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil // no state file yet
		}
		return nil, err
	}
	defer f.Close()
	json.NewDecoder(f).Decode(state)
	return state, nil
}

func SaveUploadState(path string, state *UploadState) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(state)
}
