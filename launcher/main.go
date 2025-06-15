package main

import (
	"crypto/md5"
	"encoding/base32"
	"encoding/json"
	"fmt"
        "io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"syscall"

        "github.com/justincormack/go-memfd"
)

type JsonRaw = map[string]interface{}
type Thunk func() string

type EnvVar struct {
	Type string
	Key  string
	Or   Thunk
}

func NewEnvVar(raw JsonRaw) (e EnvVar) {
	e.Type = "env"
	e.Key = raw["key"].(string)
	if or, ok := raw["or"]; ok {
		e.Or = func() string {
			return valToString(or)
		}
	}
	return
}

func (e EnvVar) String() string {
	r, _ := os.LookupEnv(e.Key)
	if r != "" {
		return r
	}
	if e.Or != nil {
		return e.Or()
	}
	panic(fmt.Sprintf("environment variable '%s' not set", e.Key))
}

type Concat struct {
	Type string
	A    string
	B    string
}

func NewConcat(raw JsonRaw) (c Concat) {
	c.Type = "concat"
	c.A = valToString(raw["a"])
	c.B = valToString(raw["b"])
	return
}

func (c Concat) String() string {
	return c.A + c.B
}

type InstanceId struct {
	Type string
	Id   string
}

func FindInstanceId() string {
	var sum = md5.Sum([]byte(strconv.Itoa(os.Getpid())))
	var enc = base32.NewEncoding("0123456789abcdfghijklmnpqrsvwxyz").WithPadding(base32.NoPadding)
	return enc.EncodeToString(sum[:])
}

func NewInstanceId(raw JsonRaw) (i InstanceId) {
	i.Type = "instanceId"
	i.Id = FindInstanceId()
	return
}

func (i InstanceId) String() string {
	return i.Id
}

type Mkdir struct {
	Type string
	Dir  string
}

func NewMkdir(raw JsonRaw) (m Mkdir) {
	m.Type = "mkdir"
	m.Dir = valToString(raw["dir"])
	return
}

func (m Mkdir) String() string {
	err := os.MkdirAll(m.Dir, 0700)
	if err != nil {
		fmt.Println(err)
	}
	return m.Dir
}

func envOr(name string, or string) string {
	val, found := os.LookupEnv(name)
	if found {
		return val
	} else {
		return or
	}
}

func valToString(item interface{}) (ret string) {
	ret, ok := item.(string)
	if ok {
		return
	} else {
		raw, _ := item.(JsonRaw)
		switch raw["type"] {
		case "env":
			ret = NewEnvVar(raw).String()
		case "concat":
			ret = NewConcat(raw).String()
		case "instanceId":
			ret = NewInstanceId(raw).String()
		case "mkdir":
			ret = NewMkdir(raw).String()
		default:
			panic("Unknown type: \"" + raw["type"].(string) + "\"")
		}
		return
	}
}

func readJsonArgs(filename string) (args []string) {
	file, _ := os.Open(filename)
	bytes, _ := ioutil.ReadAll(file)
	var argsRaw []interface{}

	json.Unmarshal(bytes, &argsRaw)
	for _, item := range argsRaw {
		args = append(args, valToString(item))
	}
	return
}

func main() {
	bwrapExe := envOr("BWRAP_EXE", "bwrap")

	bwrapArgsJson, foundBwrapArgs := os.LookupEnv("BUBBLEWRAP_ARGS")
	if !foundBwrapArgs {
		panic("No bubblewrap args given")
	}

	xdgRuntimeDir, foundXdgRuntimeDir := os.LookupEnv("XDG_RUNTIME_DIR")
	if !foundXdgRuntimeDir {
		panic("No XDG_RUNTIME_DIR set")
	}

	dbusproxyArgsJson, useDbusProxy := os.LookupEnv("XDG_DBUS_PROXY_ARGS")

	appExe, foundAppExe := os.LookupEnv("NIXPAK_APP_EXE")
	if !foundAppExe {
		panic("No executable given")
	}

	var r, w *os.File
	var dbusproxyExe string
	if useDbusProxy {
		dbusproxyExe = envOr("XDG_DBUS_PROXY_EXE", "xdg-dbus-proxy")
		var err error
		r, w, err = os.Pipe()
		if err != nil {
			panic(err)
		}
	}

	bwrapArgs := readJsonArgs(bwrapArgsJson)
	if useDbusProxy {
		bwrapArgs = append([]string{"--sync-fd", strconv.Itoa(int(r.Fd()))}, bwrapArgs...)
	}

        flatpakInfoPath, foundFlatpakInfo := os.LookupEnv("FLATPAK_INFO")
        if !foundFlatpakInfo {
                panic("No Flatpak info given")
        }

        flatpakInfo, err := os.Open(flatpakInfoPath)
        if err != nil {
                panic(err)
        }

        flatpakInfoPatched, err := memfd.Create()
        if err != nil {
                panic(err)
        }

        io.Copy(flatpakInfoPatched, flatpakInfo)

        instanceId := FindInstanceId()
        if _, err := flatpakInfoPatched.WriteString("[Instance]\ninstance-id=" + instanceId + "\n"); err != nil {
                panic(err)
        }

        _, err = flatpakInfoPatched.Seek(0, io.SeekStart)
        if err != nil {
                panic(err)
        }

        err = flatpakInfoPatched.SetImmutable()
        if err != nil {
                panic(err)
        }

        if err := os.MkdirAll(xdgRuntimeDir + "/.flatpak/" + instanceId, 0700); err != nil {
                panic(err)
        }

        bwrapInfo, err := os.OpenFile(xdgRuntimeDir + "/.flatpak/" + instanceId + "/bwrapinfo.json", os.O_CREATE | os.O_RDWR | os.O_TRUNC, 0644)
        if err != nil {
                panic(err)
        }

        bwrapArgs = append(bwrapArgs, "--ro-bind-data", strconv.Itoa(int(flatpakInfoPatched.Fd())), "/.flatpak-info")

	bwrapArgs = append(bwrapArgs, "--")
	bwrapArgs = append(bwrapArgs, appExe)
	bwrapArgs = append(bwrapArgs, os.Args[1:]...)

	if useDbusProxy {
		dbusproxyArgs := readJsonArgs(dbusproxyArgsJson)
		dbus := exec.Command(dbusproxyExe, append([]string{"--fd=3"}, dbusproxyArgs...)...)
		dbus.Stdout = os.Stdout
		dbus.Stderr = os.Stderr

		dbus.ExtraFiles = []*os.File{w, flatpakInfoPatched.File, bwrapInfo}

		if err := dbus.Start(); err != nil {
			panic(err)
		}

		w.Close()
		if _, err := r.Read([]byte{'x'}); err != nil {
			panic(err)
		}
	}

	// unset O_CLOEXEC
	syscall.Syscall(syscall.SYS_FCNTL, r.Fd(), syscall.F_SETFD, 0)
	syscall.Syscall(syscall.SYS_FCNTL, flatpakInfoPatched.Fd(), syscall.F_SETFD, 0)

	syscall.Exec(bwrapExe, append([]string{bwrapExe}, bwrapArgs...), os.Environ())
}
