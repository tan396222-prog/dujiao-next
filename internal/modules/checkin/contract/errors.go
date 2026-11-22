package contract

import "errors"

var (
	ErrAccountUnavailable = errors.New("check-in account unavailable")
	ErrFailed             = errors.New("check-in failed")
	ErrRewardOutOfRange   = errors.New("check-in reward out of range")
)
