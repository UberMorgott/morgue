package webview2

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/UberMorgott/morgue/internal/util"
	"github.com/wailsapp/go-webview2/webviewloader"
)

const (
	ResultClose    = 0
	ResultSystem   = 1
	ResultPortable = 2

	manualDownloadURL = "https://developer.microsoft.com/en-us/microsoft-edge/webview2/"

	systemBootstrapperURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
	portableCabURL        = "https://msedge.sf.dl.delivery.mp.microsoft.com/filestreamingservice/files/7a85774b-c5c9-4095-8672-53572bdc8d8f/Microsoft.WebView2.FixedVersionRuntime.148.0.3967.83.x64.cab"
)

var user32 = syscall.NewLazyDLL("user32.dll")

// LocalRuntimePath returns the path for the portable WebView2 runtime.
func LocalRuntimePath() string {
	return filepath.Join(util.BaseDir(), "webview2-runtime")
}

// CheckAvailable checks for WebView2 runtime availability.
// Returns the version string and whether it was found locally.
func CheckAvailable() (version string, isLocal bool) {
	if os.Getenv("MORGUE_TEST_NO_WEBVIEW2") == "1" {
		return "", false
	}

	// Check local portable runtime first
	v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString(LocalRuntimePath())
	if err == nil && v != "" {
		return v, true
	}

	// Check system-wide runtime
	v, err = webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err == nil && v != "" {
		return v, false
	}

	return "", false
}

// ShowInstallDialog displays a Win32 MessageBox asking the user how to install WebView2.
func ShowInstallDialog() int {
	const (
		mbYesNoCancel  = 0x00000003
		mbIconWarning  = 0x00000030
		idYes          = 6
		idNo           = 7
	)

	msgBoxW := user32.NewProc("MessageBoxW")

	text := "Microsoft WebView2 Runtime is not installed.\n" +
		"It is required for the GUI mode.\n\n" +
		"CLI mode works without WebView2:\n" +
		"  morgue run <file>\n\n" +
		"Choose installation method:\n" +
		"  [Yes] — Install system-wide (recommended)\n" +
		"  [No] — Install portable (into app folder)\n" +
		"  [Cancel] — Close application"
	title := "Morgue — WebView2 Required"

	textPtr, _ := syscall.UTF16PtrFromString(text)
	titlePtr, _ := syscall.UTF16PtrFromString(title)

	ret, _, _ := msgBoxW.Call(
		0,
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbYesNoCancel|mbIconWarning),
	)

	switch ret {
	case idYes:
		return ResultSystem
	case idNo:
		return ResultPortable
	default:
		return ResultClose
	}
}

// ShowError displays an error dialog.
func ShowError(message string) {
	showErrorDialog(message)
}

func showErrorDialog(message string) {
	const (
		mbOK        = 0x00000000
		mbIconError = 0x00000010
	)

	msgBoxW := user32.NewProc("MessageBoxW")

	textPtr, _ := syscall.UTF16PtrFromString(message)
	titlePtr, _ := syscall.UTF16PtrFromString("Morgue — Error")

	msgBoxW.Call(
		0,
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbOK|mbIconError),
	)
}

// InstallSystem downloads and runs the WebView2 bootstrapper for system-wide installation.
func InstallSystem() error {
	tmpPath := filepath.Join(util.BaseDir(), "MicrosoftEdgeWebview2Setup.exe")
	defer os.Remove(tmpPath)

	pw := showProgress("Installing WebView2")
	pw.SetStatus("Downloading installer...")

	onProgress := func(read, total int64) {
		if total > 0 {
			pw.SetProgress(int(read * 100 / total))
			pw.SetDetail(fmt.Sprintf("%s / %s", formatBytes(read), formatBytes(total)))
		} else {
			pw.SetDetail(fmt.Sprintf("%s downloaded", formatBytes(read)))
		}
	}

	if err := downloadFile(systemBootstrapperURL, tmpPath, onProgress); err != nil {
		pw.Close()
		return fmt.Errorf("download failed: %w\n\nManual download: %s", err, manualDownloadURL)
	}

	// Close progress window — bootstrapper has its own UI
	pw.Close()

	// Launch with /install (not /silent) — shows progress to user, no interaction needed
	// Use ShellExecuteExW with SEE_MASK_NOCLOSEPROCESS to get process handle for waiting
	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecuteEx := shell32.NewProc("ShellExecuteExW")

	verb, _ := syscall.UTF16PtrFromString("runas")
	exe, _ := syscall.UTF16PtrFromString(tmpPath)
	installArgs, _ := syscall.UTF16PtrFromString("/install")

	const seeMaskNoCloseProcess = 0x00000040
	const swShowNormal = 1

	type shellExecuteInfo struct {
		cbSize       uint32
		fMask        uint32
		hwnd         uintptr
		lpVerb       *uint16
		lpFile       *uint16
		lpParameters *uint16
		lpDirectory  *uint16
		nShow        int32
		hInstApp     uintptr
		lpIDList     uintptr
		lpClass      *uint16
		hkeyClass    uintptr
		dwHotKey     uint32
		hIcon        uintptr
		hProcess     syscall.Handle
	}

	sei := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       exe,
		lpParameters: installArgs,
		nShow:        swShowNormal,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))

	ret, _, _ := shellExecuteEx.Call(uintptr(unsafe.Pointer(&sei)))
	if ret == 0 {
		return fmt.Errorf("failed to launch installer\n\nManual download: %s", manualDownloadURL)
	}

	// Wait for the installer process to finish
	if sei.hProcess != 0 {
		syscall.WaitForSingleObject(sei.hProcess, syscall.INFINITE)
		syscall.CloseHandle(sei.hProcess)
	}

	return nil
}

// InstallPortable downloads the WebView2 fixed-version runtime cab and extracts it.
func InstallPortable() error {
	destDir := LocalRuntimePath()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}

	tmpPath := filepath.Join(util.BaseDir(), "webview2-runtime.cab")
	defer os.Remove(tmpPath)

	pw := showProgress("Installing WebView2 (Portable)")
	pw.SetStatus("Downloading runtime...")

	onProgress := func(read, total int64) {
		if total > 0 {
			pw.SetProgress(int(read * 100 / total))
			pw.SetDetail(fmt.Sprintf("%s / %s", formatBytes(read), formatBytes(total)))
		} else {
			pw.SetDetail(fmt.Sprintf("%s downloaded", formatBytes(read)))
		}
	}

	if err := downloadFile(portableCabURL, tmpPath, onProgress); err != nil {
		pw.Close()
		return fmt.Errorf("download failed: %w\n\nManual download: %s", err, manualDownloadURL)
	}

	pw.SetStatus("Extracting runtime...")
	pw.SetProgress(100)

	cmd := exec.Command(`C:\Windows\System32\expand.exe`, tmpPath, "-F:*", destDir)
	if err := cmd.Run(); err != nil {
		pw.Close()
		return fmt.Errorf("extract cab failed: %w\n\nManual download: %s", err, manualDownloadURL)
	}

	// After expand.exe, check if EBWebView is nested in a subdirectory
	entries, _ := os.ReadDir(destDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nested := filepath.Join(destDir, e.Name(), "EBWebView")
		if _, err := os.Stat(nested); err == nil {
			// EBWebView is inside a subdirectory — move everything up one level
			subDir := filepath.Join(destDir, e.Name())
			subEntries, _ := os.ReadDir(subDir)
			for _, se := range subEntries {
				os.Rename(filepath.Join(subDir, se.Name()), filepath.Join(destDir, se.Name()))
			}
			os.Remove(subDir) // remove empty wrapper
			break
		}
	}

	pw.Close()
	return nil
}

func downloadFile(url, dest string, onProgress func(read, total int64)) error {
	var lastErr error
	backoff := 2 * time.Second

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}

		lastErr = doDownload(url, dest, onProgress)
		if lastErr == nil {
			return nil
		}
	}

	return fmt.Errorf("failed to download after 3 attempts from %s: %w", url, lastErr)
}

func doDownload(url, dest string, onProgress func(read, total int64)) error {
	client := &http.Client{Timeout: 10 * time.Minute}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	os.Remove(dest) // remove previous attempt if exists
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	var reader io.Reader = resp.Body
	if onProgress != nil {
		reader = &progressReader{
			r:        resp.Body,
			total:    resp.ContentLength,
			onUpdate: onProgress,
		}
	}

	_, err = io.Copy(f, reader)
	f.Close()
	if err != nil {
		os.Remove(dest)
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	} else if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
}
