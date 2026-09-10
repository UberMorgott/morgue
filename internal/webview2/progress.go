package webview2

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32dll   = syscall.NewLazyDLL("user32.dll")
	kernel32dll = syscall.NewLazyDLL("kernel32.dll")
	comctl32dll = syscall.NewLazyDLL("comctl32.dll")

	procRegisterClassExW     = user32dll.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32dll.NewProc("CreateWindowExW")
	procShowWindow           = user32dll.NewProc("ShowWindow")
	procUpdateWindow         = user32dll.NewProc("UpdateWindow")
	procGetMessageW          = user32dll.NewProc("GetMessageW")
	procTranslateMessage     = user32dll.NewProc("TranslateMessage")
	procDispatchMessageW     = user32dll.NewProc("DispatchMessageW")
	procDefWindowProcW       = user32dll.NewProc("DefWindowProcW")
	procPostQuitMessage      = user32dll.NewProc("PostQuitMessage")
	procDestroyWindow        = user32dll.NewProc("DestroyWindow")
	procSendMessageW         = user32dll.NewProc("SendMessageW")
	procPostMessageW         = user32dll.NewProc("PostMessageW")
	procSetWindowTextW       = user32dll.NewProc("SetWindowTextW")
	procGetSystemMetrics     = user32dll.NewProc("GetSystemMetrics")
	procGetModuleHandleW     = kernel32dll.NewProc("GetModuleHandleW")
	procInitCommonControlsEx = comctl32dll.NewProc("InitCommonControlsEx")
)

const (
	wsOverlapped = 0x00000000
	wsCaption    = 0x00C00000
	wsSysMenu    = 0x00080000
	wsChild      = 0x40000000
	wsVisible    = 0x10000000
	wsExTopmost  = 0x00000008

	swShow = 5

	smCxScreen = 0
	smCyScreen = 1

	colorBtnFace = 15

	wmClose   = 0x0010
	wmDestroy = 0x0002

	pbsSmooth   = 0x01
	pbmSetRange = 0x0406 // PBM_SETRANGE32 = WM_USER+6
	pbmSetPos   = 0x0402 // PBM_SETPOS = WM_USER+2

	iccProgressClass = 0x00000020
)

type wndClassExW struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   syscall.Handle
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type point struct {
	x, y int32
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type initCommonControlsExInfo struct {
	size uint32
	icc  uint32
}

type progressWindow struct {
	hwnd       uintptr
	labelHwnd  uintptr
	barHwnd    uintptr
	detailHwnd uintptr
	done       chan struct{}
}

// showProgress creates and shows a native Win32 progress window.
// The message pump runs on a dedicated OS thread in a separate goroutine.
func showProgress(title string) *progressWindow {
	pw := &progressWindow{
		done: make(chan struct{}),
	}

	ready := make(chan struct{})

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// Initialize common controls for progress bar
		icc := initCommonControlsExInfo{
			size: uint32(unsafe.Sizeof(initCommonControlsExInfo{})),
			icc:  iccProgressClass,
		}
		//nolint:gosec // G103: INITCOMMONCONTROLSEX is passed by pointer — the Win32
		// calling convention; no pointer arithmetic.
		_, _, _ = procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

		hInstance, _, _ := procGetModuleHandleW.Call(0)

		className, _ := syscall.UTF16PtrFromString("MorgueProgressClass")

		wc := wndClassExW{
			size:       uint32(unsafe.Sizeof(wndClassExW{})),
			wndProc:    syscall.NewCallback(progressWndProc),
			instance:   syscall.Handle(hInstance),
			background: colorBtnFace + 1,
			className:  className,
		}

		//nolint:gosec // G103: WNDCLASSEX is passed by pointer — the Win32 calling
		// convention; no pointer arithmetic.
		_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

		// Get screen dimensions for centering
		screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
		screenH, _, _ := procGetSystemMetrics.Call(smCyScreen)

		winW := uintptr(420)
		winH := uintptr(150)
		x := (screenW - winW) / 2
		y := (screenH - winH) / 2

		titlePtr, _ := syscall.UTF16PtrFromString(title)

		//nolint:gosec // G103: CreateWindowExW takes UTF-16 class/title strings as
		// raw pointers; syscall.UTF16PtrFromString results are kept alive by locals.
		pw.hwnd, _, _ = procCreateWindowExW.Call(
			wsExTopmost,
			uintptr(unsafe.Pointer(className)),
			uintptr(unsafe.Pointer(titlePtr)),
			uintptr(wsOverlapped|wsCaption|wsSysMenu),
			x, y, winW, winH,
			0, 0, hInstance, 0,
		)

		// Status label
		staticClass, _ := syscall.UTF16PtrFromString("STATIC")
		emptyText, _ := syscall.UTF16PtrFromString("")

		//nolint:gosec // G103: same CreateWindowExW UTF-16 pointer convention as above.
		pw.labelHwnd, _, _ = procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(staticClass)),
			uintptr(unsafe.Pointer(emptyText)),
			uintptr(wsChild|wsVisible),
			15, 12, 380, 20,
			pw.hwnd, 0, hInstance, 0,
		)

		// Progress bar
		progressClass, _ := syscall.UTF16PtrFromString("msctls_progress32")

		//nolint:gosec // G103: same CreateWindowExW UTF-16 pointer convention as above.
		pw.barHwnd, _, _ = procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(progressClass)),
			0,
			uintptr(wsChild|wsVisible|pbsSmooth),
			15, 40, 380, 22,
			pw.hwnd, 0, hInstance, 0,
		)

		// Set progress bar range 0-100 (PBM_SETRANGE32: wParam=min, lParam=max)
		_, _, _ = procSendMessageW.Call(pw.barHwnd, pbmSetRange, 0, 100)

		// Detail label
		//nolint:gosec // G103: same CreateWindowExW UTF-16 pointer convention as above.
		pw.detailHwnd, _, _ = procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(staticClass)),
			uintptr(unsafe.Pointer(emptyText)),
			uintptr(wsChild|wsVisible),
			15, 70, 380, 20,
			pw.hwnd, 0, hInstance, 0,
		)

		_, _, _ = procShowWindow.Call(pw.hwnd, swShow)
		_, _, _ = procUpdateWindow.Call(pw.hwnd)

		close(ready)

		// Message pump
		var m msg
		for {
			//nolint:gosec // G103: passing the MSG struct by pointer is GetMessageW's
			// calling convention.
			ret, _, _ := procGetMessageW.Call(
				uintptr(unsafe.Pointer(&m)),
				0, 0, 0,
			)
			// 0 = WM_QUIT, all-ones = -1 = error. Compared as uintptr so no
			// narrowing conversion is needed.
			if ret == 0 || ret == ^uintptr(0) {
				break
			}
			//nolint:gosec // G103: same MSG-by-pointer convention as GetMessageW above.
			_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			//nolint:gosec // G103: same MSG-by-pointer convention as GetMessageW above.
			_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}

		close(pw.done)
	}()

	<-ready
	return pw
}

func progressWndProc(hwnd uintptr, umsg uint32, wParam, lParam uintptr) uintptr {
	switch umsg {
	case wmClose:
		_, _, _ = procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	default:
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(umsg), wParam, lParam)
		return ret
	}
}

// SetStatus updates the main status text.
func (p *progressWindow) SetStatus(text string) {
	ptr, _ := syscall.UTF16PtrFromString(text)
	//nolint:gosec // G103: SetWindowTextW takes a UTF-16 string as a raw pointer.
	_, _, _ = procSetWindowTextW.Call(p.labelHwnd, uintptr(unsafe.Pointer(ptr)))
}

// SetDetail updates the detail text (e.g. bytes downloaded).
func (p *progressWindow) SetDetail(text string) {
	ptr, _ := syscall.UTF16PtrFromString(text)
	//nolint:gosec // G103: SetWindowTextW takes a UTF-16 string as a raw pointer.
	_, _, _ = procSetWindowTextW.Call(p.detailHwnd, uintptr(unsafe.Pointer(ptr)))
}

// SetProgress sets the progress bar position (0-100).
func (p *progressWindow) SetProgress(percent int) {
	_, _, _ = procSendMessageW.Call(p.barHwnd, pbmSetPos, uintptr(percent), 0)
}

// Close destroys the progress window.
// Uses PostMessageW(WM_CLOSE) instead of DestroyWindow because
// the window must be destroyed from its owning thread.
func (p *progressWindow) Close() {
	_, _, _ = procPostMessageW.Call(p.hwnd, wmClose, 0, 0)
	<-p.done
}
