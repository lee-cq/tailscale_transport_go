package logger

// https://github.com/EddieIvan01/iox/blob/master/logger/logger.go
import (
	"fmt"
	"os"
)

const (
	pWARN    = "[!] "
	pINFO    = "[+] "
	pSUCCESS = "[*] "
)

func Info(format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, pINFO+format+"\n", args...)
}

func Warn(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, pWARN+format+"\n", args...)
}

func Success(format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, pSUCCESS+format+"\n", args...)
}
