package fsm

type UserStatus int

const (
	None UserStatus = iota
	Sub
	UnSub
)
