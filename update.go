package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const releasesAPI = "https://api.github.com/repos/Lorenzo2345612/claude-workspaces/releases/latest"

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

// cmdUpdate replaces this binary with the latest release: cw update [--check] [--force].
func cmdUpdate(args []string) error {
	check, force := false, false
	for _, a := range args {
		switch a {
		case "--check":
			check = true
		case "--force":
			force = true
		default:
			return fmt.Errorf("opción desconocida %q (usa --check o --force)", a)
		}
	}
	rel, err := latestRelease()
	if err != nil {
		return fmt.Errorf("no pude consultar la última versión: %w", err)
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	fmt.Printf("instalada: %s · última: %s\n", version, latest)
	switch {
	case version == "dev" && !force:
		fmt.Println("esta es una versión de desarrollo; usa cw update --force para reemplazarla por la publicada")
		return nil
	case !force && !newerVersion(latest, version):
		fmt.Println("ya tienes la última versión")
		return nil
	case check:
		fmt.Println("hay una versión nueva: corre cw update para instalarla")
		return nil
	}

	pkgOS := map[string]string{"linux": "linux", "darwin": "macos"}[runtime.GOOS]
	if pkgOS == "" {
		return fmt.Errorf("sistema no soportado: %s", runtime.GOOS)
	}
	pkgName := fmt.Sprintf("cw-%s-%s.tar.gz", latest, pkgOS)
	pkgURL, sumsURL := "", ""
	for _, a := range rel.Assets {
		switch {
		case a.Name == pkgName:
			pkgURL = a.URL
		case strings.HasSuffix(a.Name, "SHA256SUMS.txt"):
			sumsURL = a.URL
		}
	}
	if pkgURL == "" {
		return fmt.Errorf("el release %s no trae %s", rel.TagName, pkgName)
	}

	fmt.Printf("descargando %s…\n", pkgName)
	pkg, err := download(pkgURL)
	if err != nil {
		return err
	}
	if sumsURL != "" {
		sums, err := download(sumsURL)
		if err != nil {
			return err
		}
		if err := verifySHA256(pkg, pkgName, sums); err != nil {
			return err
		}
	}
	binName := fmt.Sprintf("cw-%s-%s", runtime.GOOS, runtime.GOARCH)
	bin, err := extractFromTarGz(pkg, binName)
	if err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	if err := replaceFile(self, bin); err != nil {
		return fmt.Errorf("no pude reemplazar %s: %w", self, err)
	}
	fmt.Printf("cw actualizado a %s en %s\n", latest, self)
	if names := runningWorkspaces(); len(names) > 0 {
		fmt.Printf("workspaces abiertos (%s): desconéctate y vuelve a entrar para usar la versión nueva\n",
			strings.Join(names, ", "))
	}
	return nil
}

func latestRelease() (*githubRelease, error) {
	req, err := http.NewRequest("GET", releasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub respondió %s", resp.Status)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func download(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("descarga de %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

func verifySHA256(data []byte, name string, sums []byte) error {
	got := sha256.Sum256(data)
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if f[0] != hex.EncodeToString(got[:]) {
				return fmt.Errorf("la suma SHA-256 de %s no coincide: descarga dañada, no instalé nada", name)
			}
			return nil
		}
	}
	return fmt.Errorf("%s no aparece en las sumas SHA-256 del release", name)
}

// extractFromTarGz returns the contents of the file named base (any directory) in the archive.
func extractFromTarGz(data []byte, base string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("el paquete no trae %s", base)
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == base && h.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}

// replaceFile swaps path for new contents atomically (temp file in the same directory + rename),
// so a running cw keeps working and an interrupted update leaves the old binary in place.
func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cw-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// newerVersion reports whether a (e.g. "0.2.1") is newer than b, comparing dotted numbers.
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}
