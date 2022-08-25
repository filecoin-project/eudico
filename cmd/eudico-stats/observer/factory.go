package observer

const CONSOLE = "console"
const BASIC = "basic"

func MakeObserver(configs map[string]string) (Observer, error) {
	observerType := configs["type"]

	switch observerType {
	case BASIC:
		return &BasicObserver{}, nil
	case CONSOLE:
		return &ConsolePrint{}, nil
	default:
		return &ConsolePrint{}, nil
	}
}
