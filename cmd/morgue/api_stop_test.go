package main

import "testing"

func TestAPIStopCommand(t *testing.T) {
	root := apiCmd()
	cmd, _, err := root.Find([]string{"stop"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd == root || cmd.Name() != "stop" || cmd.RunE == nil {
		t.Fatal("api stop is not registered as an executable command")
	}
	if cmd.Args == nil || cmd.Args(cmd, []string{"unexpected"}) == nil {
		t.Fatal("api stop must reject positional arguments")
	}
}
