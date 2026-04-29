package orchestrate

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (inf *Infra) hostInfraPIDPath(component string) string {
	return filepath.Join(inf.Home, "run", "agency-infra-"+component+".pid")
}

func (inf *Infra) legacyHostInfraPIDPath(component string) string {
	return filepath.Join(inf.Home, "run", component+".pid")
}

func (inf *Infra) writeHostInfraPID(component string, pid int) error {
	return os.WriteFile(inf.hostInfraPIDPath(component), []byte(strconv.Itoa(pid)), 0o644)
}

func (inf *Infra) readHostInfraPID(component string) (int, error) {
	data, err := os.ReadFile(inf.hostInfraPIDPath(component))
	if errors.Is(err, os.ErrNotExist) {
		data, err = os.ReadFile(inf.legacyHostInfraPIDPath(component))
	}
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func (inf *Infra) hostInfraPID(component string) (int, bool) {
	pid, err := inf.readHostInfraPID(component)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func (inf *Infra) removeHostInfraPID(component string) {
	_ = os.Remove(inf.hostInfraPIDPath(component))
	_ = os.Remove(inf.legacyHostInfraPIDPath(component))
}
