package main

import (
	"fmt"
	"os"
	"strings"
)

var flags struct {
	action      byte
	verbose     bool
	checksum    bool
	deloriginal bool
	paths       []string
}

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}

func parseFlags() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Invalid argument(s):\n%v\n", r)
			os.Exit(1)
		}
	}()

	args := os.Args
	flags.paths = make([]string, 0)

	for i := 1; i < len(args); i++ {
		arg := args[i]
		if _, err := os.Stat(arg); err == nil {
			if strings.HasSuffix(arg, "/") {
				arg = arg[:len(arg)-1]
			}
			flags.paths = append(flags.paths, arg)
			continue
		}

		if strings.HasPrefix(arg, "-") {
			arg = arg[1:]
		}

		for _, p := range arg {
			switch p {
			case 'a', 'x', 'l', 'w':
				if flags.action != 0 {
					panicf("conflict arguments: %s and %s", string(p), string(flags.action))
				}
				flags.action = byte(p)
			case 'v':
				flags.verbose = true
			case 'X':
				flags.deloriginal = true
			case 'k':
				flags.checksum = true
			default:
				panicf("unknown command: %s in %s", string(p), arg)
			}
		}
	}

	if flags.action == 'l' {
		flags.verbose = true
	}

	if flags.action == 'a' {
		for _, path := range flags.paths {
			st, _ := os.Stat(path)
			if !st.IsDir() {
				panicf("can't archive single file: %s", path)
			}
		}
	}

	if len(flags.paths) == 0 {
		panicf("please provide at least one path")
	}
}

func fmtPrintln(args ...interface{}) {
	if !flags.verbose {
		return
	}
	fmt.Println(args...)
}

func fmtPrintf(format string, args ...interface{}) {
	if !flags.verbose {
		return
	}
	fmt.Printf(format, args...)
}

func fmtPrintferr(format string, args ...interface{}) {
	os.Stderr.WriteString(fmt.Sprintf(format, args...))
}
