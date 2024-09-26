package tunnel

import (
	"errors"
	"strings"
)

type Mode int

const (
	Local Mode = iota
	Remote
)

func (m *Mode) UnmarshalTOML(data interface{}) error {
	s, ok := data.(string)
	if !ok {
		return errors.New("invalid mode type")
	}

	switch strings.ToLower(s) {
	case "local", "l", "-l":
		*m = Local
	case "remote", "r", "-r":
		*m = Remote
	default:
		return errors.New("invalid mode")
	}

	return nil
}

func (m Mode) String() string {
	if m == Local {
		return "->"
	}
	return "<-"
}