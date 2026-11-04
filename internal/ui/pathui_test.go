package ui

// Tests never touch the real PATH or install anything: every App built in
// this package gets an in-memory switch.
func init() { newPathOps = func() pathOps { return &fakePath{} } }

type fakePath struct {
	on, legacy, installed bool
	adds                  int
}

func (f *fakePath) Has() bool              { return f.on }
func (f *fakePath) Add() error             { f.on, f.legacy = true, false; f.adds++; return nil }
func (f *fakePath) Remove() error          { f.on = false; return nil }
func (f *fakePath) Dir() (string, error)   { return `C:\Users\x\AppData\Local\Programs\aniclitr`, nil }
func (f *fakePath) NeedsMigration() bool   { return f.legacy }
func (f *fakePath) RunningInstalled() bool { return f.installed }
