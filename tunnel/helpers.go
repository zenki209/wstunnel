package tunnel

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/inconshreveable/log15.v2"
)

var VV string

func SetVV(vv string) { VV = vv }

func writePid(file string) {
	if file != "" {
		_ = os.Remove(file)
		pid := os.Getpid()
		f, err := os.Create(file)
		if err != nil {
			log15.Crit("Can't create pidfile", "file", file, "err", err.Error())
			os.Exit(1)
		}
		_, err = f.WriteString(strconv.Itoa(pid) + "\n")
		if err != nil {
			log15.Crit("Can't write to pidfile", "file", file, "err", err.Error())
			os.Exit(1)
		}
		f.Close()
	}
}

// Set logging to use the file or syslog, one of the them must be "" else an error ensues
func makeLogger(pkg, file, facility string) log15.Logger {
	log := log15.New("pkg", pkg)
	if file != "" {
		if facility != "" {
			log.Crit("Can't log to syslog and logfile simultaneously")
			os.Exit(1)
		}
		log.Info("Switching logging", "file", file)
		h, err := log15.FileHandler(file, SimpleFormat())
		if err != nil {
			log.Crit("Can't create log file", "file", file, "err", err.Error())
			os.Exit(1)
		}
		log15.Root().SetHandler(h)
		log.Info("Started logging here")
	} else if facility != "" {
		log.Info("Switching logging to syslog", "facility", facility)
		h, err := log15.SyslogHandler(facility, SimpleFormat())
		if err != nil {
			log.Crit("Can't connect to syslog", "err", err.Error())
			os.Exit(1)
		}
		log15.Root().SetHandler(h)
		log.Info("Started logging here")
	} else {
		log.Info("WStunnel starting")
	}
	return log
}

const simpleTimeFormat = "2006-01-02 15:04:05"
const simpleMsgJust = 40

func SimpleFormat() log15.Format {
	return log15.FormatFunc(func(r *log15.Record) []byte {
		b := &bytes.Buffer{}
		lvl := strings.ToUpper(r.Lvl.String())
		fmt.Fprintf(b, "[%s] %s %s ", r.Time.Format(simpleTimeFormat), lvl, r.Msg)

		// try to justify the log output for short messages
		if len(r.Ctx) > 0 && len(r.Msg) < simpleMsgJust {
			b.Write(bytes.Repeat([]byte{' '}, simpleMsgJust-len(r.Msg)))
		}
		// print the keys logfmt style
		for i := 0; i < len(r.Ctx); i += 2 {
			if i != 0 {
				b.WriteByte(' ')
			}

			k, ok := r.Ctx[i].(string)
			v := formatLogfmtValue(r.Ctx[i+1])
			if !ok {
				k, v = "LOG_ERR", formatLogfmtValue(k)
			}

			// XXX: we should probably check that all of your key bytes aren't invalid
			fmt.Fprintf(b, "%s=%s", k, v)
		}

		b.WriteByte('\n')
		return b.Bytes()
	})
}

// copied from log15 https://github.com/inconshreveable/log15/blob/master/format.go#L203-L223
func formatLogfmtValue(value interface{}) string {
	if value == nil {
		return "nil"
	}

	value = formatShared(value)
	switch v := value.(type) {
	case bool:
		return strconv.FormatBool(v)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', 3, 64)
	case float64:
		return strconv.FormatFloat(v, 'f', 3, 64)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", value)
	case string:
		return escapeString(v)
	default:
		return escapeString(fmt.Sprintf("%+v", value))
	}
}

// copied from log15 https://github.com/inconshreveable/log15/blob/master/format.go
func formatShared(value interface{}) (result interface{}) {
	defer func() {
		if err := recover(); err != nil {
			if v := reflect.ValueOf(value); v.Kind() == reflect.Ptr && v.IsNil() {
				result = "nil"
			} else {
				panic(err)
			}
		}
	}()

	switch v := value.(type) {
	case time.Time:
		return v.Format(simpleTimeFormat)

	case error:
		return v.Error()

	case fmt.Stringer:
		return v.String()

	default:
		return v
	}
}

// copied from log15 https://github.com/inconshreveable/log15/blob/master/format.go
func escapeString(s string) string {
	needQuotes := false
	e := bytes.Buffer{}
	e.WriteByte('"')
	for _, r := range s {
		if r <= ' ' || r == '=' || r == '"' {
			needQuotes = true
		}

		switch r {
		case '\\', '"':
			e.WriteByte('\\')
			e.WriteByte(byte(r))
		case '\n':
			e.WriteByte('\\')
			e.WriteByte('n')
		case '\r':
			e.WriteByte('\\')
			e.WriteByte('r')
		case '\t':
			e.WriteByte('\\')
			e.WriteByte('t')
		default:
			e.WriteRune(r)
		}
	}
	e.WriteByte('"')
	start, stop := 0, e.Len()
	if !needQuotes {
		start, stop = 1, stop-1
	}
	return string(e.Bytes()[start:stop])
}

func calcWsTimeout(tout int) time.Duration {
	var wsTimeout time.Duration
	if tout < 3 {
		wsTimeout = 3 * time.Second
	} else if tout > 600 {
		wsTimeout = 600 * time.Second
	} else {
		wsTimeout = time.Duration(tout) * time.Second
	}
	log15.Info("Websocket keep-alive timeout", "timeout", wsTimeout)
	return wsTimeout
}

// copy http headers over
func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}