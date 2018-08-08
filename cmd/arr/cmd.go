package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var flags struct {
	action       byte
	verbose      bool
	checksum     bool
	deloriginal  bool
	ignoreerrors bool
	delimm       bool
	listen       string
	paths        []string
	xdest        string
}

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}

func parseFlags() {
	usage := func() {
		fmt.Printf("Usage: arr [axlwj]vXkCfL\n")
	}

	defer func() {
		if r := recover(); r != nil {
			usage()
			fmt.Printf("Invalid argument(s):\n%v\n", r)
			os.Exit(1)
		}
	}()

	args := os.Args
	flags.paths = make([]string, 0)
	flags.xdest, _ = filepath.Abs(".")
	nextIs := '\x00'

	for i := 1; i < len(args); i++ {
		arg := args[i]
		if _, err := os.Stat(arg); err == nil {
			arg, _ = filepath.Abs(arg)
			arg = strings.Replace(arg, "\\", "/", -1)

			if strings.HasSuffix(arg, "/") {
				arg = arg[:len(arg)-1]
			}

			if nextIs == 'd' {
				nextIs = 0
				flags.xdest = arg
			} else {
				flags.paths = append(flags.paths, arg)
			}
			continue
		}

		switch nextIs {
		case 'd':
			nextIs = 0
			flags.xdest = arg
			continue
		case 'l':
			nextIs = 0
			flags.listen = arg
			continue
		}

		for strings.HasPrefix(arg, "-") {
			arg = arg[1:]
		}

		if arg == "help" {
			usage()
			os.Exit(0)
		}

		for _, p := range arg {
			switch p {
			case 'a', 'x', 'l', 'w', 'W', 'j':
				if flags.action != 0 {
					panicf("conflict arguments: %s and %s", string(p), string(flags.action))
				}
				flags.action = byte(p)
			case 'v':
				flags.verbose = true
			case 'C':
				nextIs = 'd'
			case 'L':
				nextIs = 'l'
			case 'X':
				if flags.deloriginal {
					flags.delimm = true
				}
				flags.deloriginal = true
			case 'k':
				flags.checksum = true
			case 'f':
				flags.ignoreerrors = true
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

func fmtFatalErr(arg error) {
	if arg == nil {
		return
	}

	os.Stderr.WriteString(fmt.Sprintf("\nFatal error: %v\n", arg))
	os.Exit(1)
}

func fmtMaybeErr(args ...interface{}) {
	os.Stderr.WriteString("\nError: " + fmt.Sprint(args...) + "\n")
	if flags.ignoreerrors {
		return
	}
	os.Exit(1)
}
