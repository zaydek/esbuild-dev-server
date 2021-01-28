package main

import (
	"fmt"
	"io"
	"sync"
)

type Logger struct {
	m sync.Mutex
	w io.Writer
}

func NewLogger(w io.Writer) *Logger {
	return &Logger{w: w}
}

func (l *Logger) Printf(format string, args ...interface{}) {
	l.m.Lock()
	defer l.m.Unlock()
	fmt.Fprintf(l.w, format, args...)
}

func (l *Logger) Println(args ...interface{}) {
	l.m.Lock()
	defer l.m.Unlock()
	fmt.Fprintln(l.w, args...)
}
