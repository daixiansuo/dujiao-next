package pgdump

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Match libpq's keyword/value grammar, including quoted values and escaped spaces.
// Keep the original spans: reconstructing from pgconn.Config would lose SSL and
// libpq options that pgx does not represent.
var keywordValue = regexp.MustCompile(`^[\t\n\v\f\r ]*([A-Za-z_][A-Za-z_0-9]*)[\t\n\v\f\r ]*=[\t\n\v\f\r ]*('(?:[^'\\]|\\[\s\S])*'|(?:[^\t\n\v\f\r \\']|\\[\s\S])(?:[^\t\n\v\f\r \\]|\\[\s\S])*|)`)

var pgxOnly = map[string]bool{
	"statement_cache_capacity":   true,
	"description_cache_capacity": true,
	"default_query_exec_mode":    true,
	"min_protocol_version":       true,
	"max_protocol_version":       true,
}

type connectionArgs struct {
	dsn     string
	env     map[string]string
	secrets []string
}

func prepareDSN(dsn string) (connectionArgs, error) {
	out := connectionArgs{env: make(map[string]string)}
	secret := func(key, value string) bool {
		switch key {
		case "password", "sslpassword":
			name := "PGPASSWORD"
			if key == "sslpassword" {
				name = "PGSSLPASSWORD"
			}
			out.env[name] = value
			if value != "" {
				out.secrets = append(out.secrets, value)
			}
			return true
		case "servicefile":
			out.env["PGSERVICEFILE"] = value
			return true
		}
		return false
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil || u.Fragment != "" {
			return out, errors.New("invalid PostgreSQL connection URL")
		}
		if u.User != nil {
			if password, ok := u.User.Password(); ok {
				secret("password", password)
				u.User = url.User(u.User.Username())
			}
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return out, errors.New("invalid PostgreSQL connection parameters")
		}
		for key, values := range q {
			// pgx uses the first value of a repeated URL parameter.
			secret(key, values[0])
			if key == "password" || key == "sslpassword" || key == "servicefile" || pgxOnly[key] {
				q.Del(key)
			}
		}
		u.RawQuery = q.Encode()
		out.dsn = u.String()
		return out, nil
	}

	var spans []string
	for strings.TrimSpace(dsn) != "" {
		m := keywordValue.FindStringSubmatchIndex(dsn)
		if m == nil || (m[1] < len(dsn) && !isSpace(dsn[m[1]])) {
			return out, errors.New("invalid PostgreSQL keyword/value connection string")
		}
		key, raw := dsn[m[2]:m[3]], dsn[m[4]:m[5]]
		value := unescapeValue(raw)
		if !secret(key, value) && !pgxOnly[key] {
			if key == "database" { // pgx alias; libpq calls it dbname.
				spans = append(spans, "dbname="+raw)
			} else {
				spans = append(spans, strings.TrimSpace(dsn[:m[1]]))
			}
		}
		dsn = dsn[m[1]:]
	}
	out.dsn = strings.Join(spans, " ")
	return out, nil
}

func isSpace(b byte) bool { return strings.ContainsRune(" \t\n\v\f\r", rune(b)) }

func unescapeValue(s string) string {
	if len(s) >= 2 && s[0] == '\'' {
		s = s[1 : len(s)-1]
	}
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		out.WriteByte(s[i])
	}
	return out.String()
}
