package proc

import (
	"runtime"
	"sort"
	"strings"
)

// baseEnv is what an upstream inherits from the daemon. Everything else,
// credentials included, must come from the service definition's env.
var baseEnv = []string{
	"PATH", "PATHEXT", "HOME", "USER", "USERNAME", "USERPROFILE", "USERDOMAIN",
	"HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "ALLUSERSPROFILE",
	"PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMW6432", "COMMONPROGRAMFILES",
	"SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "COMSPEC", "OS",
	"PROCESSOR_ARCHITECTURE", "NUMBER_OF_PROCESSORS",
	"TEMP", "TMP", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE", "TZ",
	"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY",
}

func envKey(k string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(k) // Windows variable names are case-insensitive
	}
	return k
}

// BuildEnv returns the allowlisted subset of parent with extra layered on top.
// Proxy variables are matched case-insensitively on every platform because
// lowercase http_proxy is common on Unix.
func BuildEnv(parent []string, extra map[string]string) []string {
	allow := map[string]bool{}
	for _, k := range baseEnv {
		allow[strings.ToUpper(k)] = true
	}
	out := map[string]string{} // envKey -> KEY=VALUE
	for _, kv := range parent {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" || !allow[strings.ToUpper(k)] {
			continue
		}
		if runtime.GOOS != "windows" && k != strings.ToUpper(k) && !strings.HasSuffix(strings.ToLower(k), "_proxy") {
			continue
		}
		out[envKey(k)] = kv
	}
	for k, v := range extra {
		out[envKey(k)] = k + "=" + v
	}
	env := make([]string, 0, len(out))
	for _, kv := range out {
		env = append(env, kv)
	}
	sort.Strings(env)
	return env
}

func lookupEnv(env []string, key string) string {
	key = envKey(key)
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && envKey(k) == key {
			return v
		}
	}
	return ""
}
