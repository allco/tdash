package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Object is a decoded JSON object that remembers its key order, so tables can show the columns
// in the order the data has them.
type Object struct {
	Keys []string
	Vals map[string]any
}

func (o *Object) Get(k string) (any, bool) {
	v, ok := o.Vals[k]
	return v, ok
}

// decodeJSON decodes one JSON value. Objects become *Object, arrays []any, numbers float64.
func decodeJSON(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := t.(type) {
	case json.Delim:
		if t == '{' {
			obj := &Object{Vals: map[string]any{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k := kt.(string)
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				if _, dup := obj.Vals[k]; !dup {
					obj.Keys = append(obj.Keys, k)
				}
				obj.Vals[k] = v
			}
			_, err := dec.Token()
			return obj, err
		}
		arr := []any{}
		for dec.More() {
			v, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		_, err := dec.Token()
		return arr, err
	case json.Number:
		return t.Float64()
	default:
		return t, nil
	}
}

// parseOutput turns raw supplier output into data, according to format.
func parseOutput(b []byte, format string) (any, error) {
	switch format {
	case "text":
		return string(b), nil
	case "lines":
		s := strings.TrimRight(string(b), "\n")
		out := []any{}
		if s != "" {
			for _, l := range strings.Split(s, "\n") {
				out = append(out, strings.TrimRight(l, "\r"))
			}
		}
		return out, nil
	case "auto":
		if v, err := decodeJSON(b); err == nil {
			return v, nil
		}
		return string(b), nil
	default:
		v, err := decodeJSON(b)
		if err != nil {
			return nil, fmt.Errorf("output is not valid JSON: %w", err)
		}
		return v, nil
	}
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces ${VAR} with the environment variable. Plain $VAR is left alone, so shell
// commands keep working as written.
func expandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		return os.Getenv(m[2 : len(m)-1])
	})
}

// fetch loads the data for one source. dir is the config file's directory.
func fetch(ctx context.Context, d DataRef, dir string) (any, error) {
	switch {
	case len(d.Inline) > 0:
		return decodeJSON(d.Inline)
	case d.File != "":
		path := d.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		format := d.Format
		if format == "" {
			format = "text"
			if strings.EqualFold(filepath.Ext(path), ".json") {
				format = "json"
			}
		}
		return parseOutput(b, format)
	case d.Command != nil:
		b, err := runCommand(ctx, d.Source, dir)
		if err != nil {
			return nil, err
		}
		return parseOutput(b, d.Format)
	case d.URL != "":
		b, err := fetchURL(ctx, d.Source)
		if err != nil {
			return nil, err
		}
		return parseOutput(b, d.Format)
	}
	return nil, nil
}

func runCommand(ctx context.Context, s Source, dir string) ([]byte, error) {
	var cmd *exec.Cmd
	if s.Command.Argv != nil {
		argv := make([]string, len(s.Command.Argv))
		for i, a := range s.Command.Argv {
			argv[i] = expandEnv(a)
		}
		cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
	} else {
		shell, flag := shellCommand()
		cmd = exec.CommandContext(ctx, shell, flag, expandEnv(s.Command.Shell))
	}
	cmd.Dir = dir
	if s.Cwd != "" {
		cmd.Dir = s.Cwd
		if !filepath.IsAbs(s.Cwd) {
			cmd.Dir = filepath.Join(dir, s.Cwd)
		}
	}
	cmd.Env = os.Environ()
	for k, v := range s.Env {
		cmd.Env = append(cmd.Env, k+"="+expandEnv(v))
	}
	// Don't wait forever for pipes held open by a child the command left behind.
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s: timed out", s.Command)
	}
	if err != nil {
		msg := fmt.Sprintf("%s: %v", s.Command, err)
		if e := firstLines(stderr.String(), 3); e != "" {
			msg += ": " + e
		}
		return nil, errors.New(msg)
	}
	return stdout.Bytes(), nil
}

func shellCommand() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh, "-c"
	}
	return "/bin/sh", "-c"
}

const maxBody = 16 << 20

func fetchURL(ctx context.Context, s Source) ([]byte, error) {
	method := s.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	contentType := ""
	if len(s.Body) > 0 {
		var str string
		if json.Unmarshal(s.Body, &str) == nil {
			body = strings.NewReader(expandEnv(str))
		} else {
			body = strings.NewReader(expandEnv(string(s.Body)))
			contentType = "application/json"
		}
	}
	url := expandEnv(s.URL)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range s.Headers {
		req.Header.Set(k, expandEnv(v))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("%s: timed out", url)
		}
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		msg := fmt.Sprintf("%s: HTTP %s", url, resp.Status)
		if e := firstLines(string(b), 2); e != "" {
			msg += ": " + e
		}
		return nil, errors.New(msg)
	}
	return b, nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " ")
}

// selectStep is one part of a select path: a field, an index, [*] or length.
type selectStep struct {
	field string
	index int
	all   bool
	isIdx bool
}

// parseSelect reads the small JSONPath subset: $.a.b, $.items[0], $.items[*].name, .length.
func parseSelect(s string) ([]selectStep, error) {
	if !strings.HasPrefix(s, "$") {
		return nil, errors.New(`a select path starts with "$"`)
	}
	var steps []selectStep
	rest := s[1:]
	for rest != "" {
		switch rest[0] {
		case '.':
			end := strings.IndexAny(rest[1:], ".[")
			if end < 0 {
				end = len(rest) - 1
			}
			name := rest[1 : 1+end]
			if name == "" {
				return nil, fmt.Errorf("empty field name in %q", s)
			}
			steps = append(steps, selectStep{field: name})
			rest = rest[1+end:]
		case '[':
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return nil, fmt.Errorf("missing ] in %q", s)
			}
			in := rest[1:end]
			if in == "*" {
				steps = append(steps, selectStep{all: true})
			} else {
				var n int
				if _, err := fmt.Sscanf(in, "%d", &n); err != nil || fmt.Sprint(n) != in {
					return nil, fmt.Errorf("bad index [%s] in %q", in, s)
				}
				steps = append(steps, selectStep{index: n, isIdx: true})
			}
			rest = rest[end+1:]
		default:
			return nil, fmt.Errorf("unexpected %q in %q", rest[0], s)
		}
	}
	return steps, nil
}

func applySelect(v any, steps []selectStep) (any, error) {
	for i, st := range steps {
		switch {
		case st.all:
			arr, ok := v.([]any)
			if !ok {
				return nil, errors.New("[*] on something that is not an array")
			}
			out := make([]any, len(arr))
			for j, el := range arr {
				r, err := applySelect(el, steps[i+1:])
				if err != nil {
					return nil, err
				}
				out[j] = r
			}
			return out, nil
		case st.isIdx:
			arr, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("[%d] on something that is not an array", st.index)
			}
			idx := st.index
			if idx < 0 {
				idx += len(arr)
			}
			if idx < 0 || idx >= len(arr) {
				return nil, fmt.Errorf("index [%d] out of range (length %d)", st.index, len(arr))
			}
			v = arr[idx]
		default:
			if obj, ok := v.(*Object); ok {
				if f, ok := obj.Get(st.field); ok {
					v = f
					continue
				}
			}
			if st.field == "length" {
				switch t := v.(type) {
				case []any:
					v = float64(len(t))
					continue
				case string:
					v = float64(len([]rune(t)))
					continue
				case *Object:
					v = float64(len(t.Keys))
					continue
				}
			}
			return nil, fmt.Errorf("no field %q", st.field)
		}
	}
	return v, nil
}
