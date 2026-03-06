package llm

import "errors"

var ErrContextWindowExceeded = errors.New("context window exceeded")

func IsContextWindowExceeded(err error) bool {
	return errors.Is(err, ErrContextWindowExceeded)
}
