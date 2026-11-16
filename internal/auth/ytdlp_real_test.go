package auth

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestRealYtDlpExportsWithoutURL runs the installed yt-dlp against a
// throwaway Firefox profile built with sqlite3, proving that the no-URL
// invocation still writes the jar. Skipped when either tool is missing.
func TestRealYtDlpExportsWithoutURL(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	for _, bin := range []string{"yt-dlp", "sqlite3"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	prof := t.TempDir()
	schema := `CREATE TABLE moz_cookies (id INTEGER PRIMARY KEY, originAttributes TEXT NOT NULL DEFAULT '',
	name TEXT, value TEXT, host TEXT, path TEXT, expiry INTEGER, lastAccessed INTEGER, creationTime INTEGER,
	isSecure INTEGER, isHttpOnly INTEGER, inBrowserElement INTEGER DEFAULT 0, sameSite INTEGER DEFAULT 0,
	rawSameSite INTEGER DEFAULT 0, schemeMap INTEGER DEFAULT 0);
	INSERT INTO moz_cookies(name,value,host,path,expiry,isSecure,isHttpOnly) VALUES
	('__Secure-3PAPISID','dummy3p','.youtube.com','/',4102444800,1,0),
	('ST-abc','big','.music.youtube.com','/',4102444800,1,0),
	('NID','x','.google.com','/',4102444800,1,1);`
	if out, err := exec.Command("sqlite3", filepath.Join(prof, "cookies.sqlite"), schema).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v %s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	jar, err := YtDlp{TempDir: t.TempDir()}.extractSpec(ctx, "firefox:"+prof)
	if err != nil {
		t.Fatal(err)
	}
	if !HasSession(jar, time.Now()) || len(jar) != 3 {
		t.Fatalf("jar = %+v", jar)
	}
	if h := CookieHeader(jar, time.Now()); h != "__Secure-3PAPISID=dummy3p" {
		t.Errorf("header = %q", h)
	}
}
