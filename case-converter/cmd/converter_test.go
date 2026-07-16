package cmd

import (
	"bytes"
	"reflect"
	"testing"
)

func TestProcessCaseConversions(t *testing.T) {
	want := map[string]string{
		"normal":        "hello world",
		"upper":         "HELLO WORLD",
		"lower":         "hello world",
		"capitalized":   "Hello world",
		"swapped":       "HELLO WORLD",
		"snake_case":    "hello_world",
		"kebab_case":    "hello-world",
		"camel_case":    "helloWorld",
		"pascal_case":   "HelloWorld",
		"constant_case": "HELLO_WORLD",
		"title_case":    "Hello World",
		"dot_case":      "hello.world",
		"path_case":     "hello/world",
		"pascal_kebab":  "Hello-World",
	}

	tests := []struct {
		name  string
		input string
	}{
		{name: "normal text and punctuation", input: "Hello World!"},
		{name: "snake case", input: "hello_world"},
		{name: "camel case", input: "helloWorld"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ProcessCaseConversions(test.input)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("ProcessCaseConversions(%q) = %#v, want %#v", test.input, got, want)
			}
		})
	}
}

func TestPrintConversions(t *testing.T) {
	var stdout bytes.Buffer
	PrintConversions(&stdout, "Hello World")

	want := "\n\033[44m\033[1;30m Original \033[0m: Hello World\n" +
		"\033[42m\033[1;30m Normal \033[0m: hello world\n" +
		"\033[42m\033[1;30m Upper \033[0m: HELLO WORLD\n" +
		"\033[42m\033[1;30m Lower \033[0m: hello world\n" +
		"\033[42m\033[1;30m Capitalized \033[0m: Hello world\n" +
		"\033[42m\033[1;30m Swapped \033[0m: HELLO WORLD\n" +
		"\033[42m\033[1;30m Snake Case \033[0m: hello_world\n" +
		"\033[42m\033[1;30m Kebab Case \033[0m: hello-world\n" +
		"\033[42m\033[1;30m Camel Case \033[0m: helloWorld\n" +
		"\033[42m\033[1;30m Pascal Case \033[0m: HelloWorld\n" +
		"\033[42m\033[1;30m Constant Case \033[0m: HELLO_WORLD\n" +
		"\033[42m\033[1;30m Title Case \033[0m: Hello World\n" +
		"\033[42m\033[1;30m Dot Case \033[0m: hello.world\n" +
		"\033[42m\033[1;30m Path Case \033[0m: hello/world\n" +
		"\033[42m\033[1;30m Pascal Kebab \033[0m: Hello-World\n"
	if stdout.String() != want {
		t.Fatalf("output = %q, want %q", stdout.String(), want)
	}
}
