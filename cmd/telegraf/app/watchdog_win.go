// +build windows

package app

// Start  windows stub
func Start() bool {
	return false
}

// StopWatch  windows stub
func StopWatch() {

}

// StopChildAndExit windows stub
func StopChildAndExit() {
}

// Restart  windows stub
func Restart(childOnly bool) {
}

type watchDog struct {
	startCount int
}

// FindProcesses windows stub,  in order not report error
func FindProcesses() ([]int, error) {
	return nil, nil
}

// Done watchdog done, windows stub
func Done() <-chan bool {
	ch := make(chan bool)
	close(ch)
	return ch
}

var wd *watchDog
