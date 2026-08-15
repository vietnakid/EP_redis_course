package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncode(t *testing.T) {
	cases := []struct {
		name string
		in   Value
		want string
	}{
		{"nil -> null bulk string", nil, "$-1\r\n"},
		{"SimpleString -> simple string", SimpleString("OK"), "+OK\r\n"},
		{"error -> simple error", errors.New("ERR bad"), "-ERR bad\r\n"},
		{"string -> bulk string", "hello", "$5\r\nhello\r\n"},
		{"empty string -> empty bulk string", "", "$0\r\n\r\n"},
		{"int64 -> integer", int64(123), ":123\r\n"},
		{"negative int64 -> integer", int64(-2), ":-2\r\n"},
		{"empty array", []Value{}, "*0\r\n"},
		{
			"flat array of mixed types",
			[]Value{"a", int64(1), nil, SimpleString("OK")},
			"*4\r\n$1\r\na\r\n:1\r\n$-1\r\n+OK\r\n",
		},
		{
			"nested array",
			[]Value{[]Value{"x", "y"}, "z"},
			"*2\r\n*2\r\n$1\r\nx\r\n$1\r\ny\r\n$1\r\nz\r\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Encode(tc.in)
			if string(got) != tc.want {
				t.Errorf("Encode(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEncodeUnsupportedTypePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Encode(unsupported type) did not panic")
		}
	}()
	Encode(3.14) // float64 has no RESP2 encoding
}

// Named wrappers must stay byte-identical to calling Encode directly -
// they are sugar over the same engine, not a second implementation.
func TestNamedWrappersMatchEncode(t *testing.T) {
	if got, want := EncodeSimpleString("PONG"), Encode(SimpleString("PONG")); !bytes.Equal(got, want) {
		t.Errorf("EncodeSimpleString = %q, want %q", got, want)
	}
	if got, want := EncodeBulkString("bar"), Encode("bar"); !bytes.Equal(got, want) {
		t.Errorf("EncodeBulkString = %q, want %q", got, want)
	}
	if got, want := EncodeError("ERR nope"), Encode(errors.New("ERR nope")); !bytes.Equal(got, want) {
		t.Errorf("EncodeError = %q, want %q", got, want)
	}
	if got, want := EncodeInteger(42), Encode(int64(42)); !bytes.Equal(got, want) {
		t.Errorf("EncodeInteger = %q, want %q", got, want)
	}
}

func TestParseCommandMultibulk(t *testing.T) {
	buf := []byte("*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n")
	cmd, consumed, err := ParseCommand(buf)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if consumed != len(buf) {
		t.Errorf("consumed = %d, want %d", consumed, len(buf))
	}
	if cmd.Name != "SET" {
		t.Errorf("cmd.Name = %q, want SET", cmd.Name)
	}
	if want := []string{"foo", "bar"}; !equalStrings(cmd.Args, want) {
		t.Errorf("cmd.Args = %v, want %v", cmd.Args, want)
	}
}

func TestParseCommandInline(t *testing.T) {
	cmd, consumed, err := ParseCommand([]byte("PING hello\r\n"))
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if consumed != len("PING hello\r\n") {
		t.Errorf("consumed = %d, want %d", consumed, len("PING hello\r\n"))
	}
	if cmd.Name != "PING" || !equalStrings(cmd.Args, []string{"hello"}) {
		t.Errorf("got Name=%q Args=%v", cmd.Name, cmd.Args)
	}
}

func TestParseCommandBlankLineHasNoName(t *testing.T) {
	cmd, consumed, err := ParseCommand([]byte("\r\nPING\r\n"))
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if consumed != 2 {
		t.Errorf("consumed = %d, want 2", consumed)
	}
	if cmd.Name != "" {
		t.Errorf("cmd.Name = %q, want empty for a blank line", cmd.Name)
	}
}

func TestParseCommandIncompleteDoesNotConsume(t *testing.T) {
	// A multibulk header promising 3 elements but only the command name
	// has fully arrived - the payload for "foo" is cut short.
	buf := []byte("*3\r\n$3\r\nSET\r\n$3\r\nfo")
	_, consumed, err := ParseCommand(buf)
	if err != ErrIncomplete {
		t.Fatalf("err = %v, want ErrIncomplete", err)
	}
	if consumed != 0 {
		t.Errorf("consumed = %d, want 0 on ErrIncomplete", consumed)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
