package javascript

import (
	"fmt"
	"os"
	"path/filepath"
)

// scratch is the policy-owned temporary tree one operation runs against: its
// working directory, its temporary directory, and every location a tool would
// otherwise read user configuration or a cache from. The exchange deletes it,
// so no policy run reads or writes the invoking user's home, and nothing an
// operation writes survives it.
type scratch struct {
	root string
	home string
	work string
	temp string
}

func newScratch() (scratch, error) {
	root, err := os.MkdirTemp("", "code-polishy-javascript-")
	if err != nil {
		return scratch{}, fmt.Errorf("create the JavaScript scratch directory: %w", err)
	}
	created := scratch{
		root: root,
		home: filepath.Join(root, "home"),
		work: filepath.Join(root, "work"),
		temp: filepath.Join(root, "tmp"),
	}
	for _, directory := range []string{created.home, created.work, created.temp} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			os.RemoveAll(root)
			return scratch{}, fmt.Errorf("create the JavaScript scratch directory: %w", err)
		}
	}
	return created, nil
}

// environment is the child's complete environment. It is built rather than
// filtered, so credential, proxy, loader, debugger, package-manager, and
// user-configuration variables are absent by construction instead of by
// blocklist. PATH is empty because every executable a policy operation runs is
// named by an absolute policy-owned path.
func (created scratch) environment() []string {
	absentConfiguration := filepath.Join(created.home, "absent-npmrc")
	return []string{
		"PATH=",
		"HOME=" + created.home,
		"TMPDIR=" + created.temp,
		"XDG_CONFIG_HOME=" + filepath.Join(created.home, "config"),
		"XDG_CACHE_HOME=" + filepath.Join(created.home, "cache"),
		"XDG_DATA_HOME=" + filepath.Join(created.home, "data"),
		"XDG_STATE_HOME=" + filepath.Join(created.home, "state"),
		"npm_config_userconfig=" + absentConfiguration,
		"npm_config_globalconfig=" + absentConfiguration,
	}
}
