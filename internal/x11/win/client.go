// +build linux

package win

import (
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil/ewmh"
	"github.com/BurntSushi/xgbutil/icccm"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xprop"

	"fyne.io/fyne"

	"fyne.io/fynedesk"
	"fyne.io/fynedesk/internal/x11"
)

type client struct {
	id, win xproto.Window

	full      bool
	iconic    bool
	maximized bool
	props     *clientProperties

	restoreX, restoreY          int16
	restoreWidth, restoreHeight uint16

	frame *frame
	wm    x11.XWM
}

// NewClient creates a new X11 client for the specified window ID and X window manager
func NewClient(win xproto.Window, wm x11.XWM) x11.XWin {
	c := &client{win: win, wm: wm}
	err := xproto.ChangeWindowAttributesChecked(wm.Conn(), win, xproto.CwEventMask,
		[]uint32{xproto.EventMaskPropertyChange | xproto.EventMaskEnterWindow | xproto.EventMaskLeaveWindow |
			xproto.EventMaskVisibilityChange}).Check()
	if err != nil {
		fyne.LogError("Could not change window attributes", err)
	}
	windowAllowedActionsSet(wm.X(), win, x11.AllowedActions)

	initialHints := x11.WindowExtendedHintsGet(wm.X(), c.win)
	for _, hint := range initialHints {
		switch hint {
		case "_NET_WM_STATE_FULLSCREEN":
			c.full = true
		case "_NET_WM_STATE_MAXIMIZED_VERT", "_NET_WM_STATE_MAXIMIZED_HORZ":
			c.maximized = true
			// TODO Handle more of these possible hints
		}
	}
	if windowStateGet(wm.X(), win) == icccm.StateIconic {
		c.iconic = true
		xproto.UnmapWindow(wm.Conn(), win)
	} else {
		c.newFrame()
	}

	return c
}

func (c *client) ChildID() xproto.Window {
	return c.win
}

func (c *client) Close() {
	winProtos, err := icccm.WmProtocolsGet(c.wm.X(), c.win)
	if err != nil {
		fyne.LogError("Get Protocols Error", err)
	}

	askNicely := false
	for _, proto := range winProtos {
		if proto == "WM_DELETE_WINDOW" {
			askNicely = true
		}
	}

	if !askNicely {
		err := xproto.DestroyWindowChecked(c.wm.Conn(), c.win).Check()
		if err != nil {
			fyne.LogError("Close Error", err)
		}

		return
	}

	protocols, err := xprop.Atm(c.wm.X(), "WM_PROTOCOLS")
	if err != nil {
		fyne.LogError("Get Protocols Error", err)
		return
	}

	delWin, err := xprop.Atm(c.wm.X(), "WM_DELETE_WINDOW")
	if err != nil {
		fyne.LogError("Get Delete Window Error", err)
		return
	}
	cm, err := xevent.NewClientMessage(32, c.win, protocols,
		int(delWin))
	err = xproto.SendEventChecked(c.wm.Conn(), false, c.win, 0,
		string(cm.Bytes())).Check()
	if err != nil {
		fyne.LogError("Window Delete Error", err)
	}
}

func (c *client) Expose() {
	if c.frame == nil {
		return
	}

	c.frame.applyTheme(false)
}

func (c *client) Focus() {
	windowActiveReq(c.wm.X(), c.win)
}

func (c *client) Focused() bool {
	active, err := x11.WindowActiveGet(c.wm.X())
	if err != nil {
		return false
	}
	return active == c.win
}

func (c *client) FrameID() xproto.Window {
	return c.id
}

func (c *client) Fullscreen() {
	c.fullscreenMessage(x11.WindowStateActionAdd)
	x11.WindowExtendedHintsAdd(c.wm.X(), c.win, "_NET_WM_STATE_FULLSCREEN")
}

func (c *client) Fullscreened() bool {
	return c.full
}

func (c *client) Iconify() {
	c.stateMessage(icccm.StateIconic)
	windowStateSet(c.wm.X(), c.win, icccm.StateIconic)
}

func (c *client) Iconic() bool {
	return c.iconic
}

func (c *client) Geometry() (int, int, uint, uint) {
	if c.frame == nil {
		return 0, 0, 0, 0
	}
	return int(c.frame.x), int(c.frame.y), uint(c.frame.width), uint(c.frame.height)
}

func (c *client) Maximize() {
	c.maximizeMessage(x11.WindowStateActionAdd)
}

func (c *client) Maximized() bool {
	return c.maximized
}

func (c *client) NotifyBorderChange() {
	if c.Properties().Decorated() {
		c.frame.addBorder()
	} else {
		c.frame.removeBorder()
	}
}

func (c *client) NotifyGeometry(x int, y int, width uint, height uint) {
	c.frame.updateGeometry(int16(x), int16(y), uint16(width), uint16(height), true)
}

func (c *client) NotifyFullscreen() {
	c.full = true
	c.frame.maximizeApply()
}

func (c *client) NotifyIconify() {
	c.frame.hide()
	c.iconic = true
	x11.WindowExtendedHintsAdd(c.wm.X(), c.win, "_NET_WM_STATE_HIDDEN")
}

func (c *client) NotifyMaximize() {
	c.maximized = true
	c.frame.maximizeApply()
	x11.WindowExtendedHintsAdd(c.wm.X(), c.win, "_NET_WM_STATE_MAXIMIZED_VERT")
	x11.WindowExtendedHintsAdd(c.wm.X(), c.win, "_NET_WM_STATE_MAXIMIZED_HORZ")
}

func (c *client) NotifyMouseDrag(x, y int16) {
	c.frame.mouseDrag(x, y)
}

func (c *client) NotifyMouseMotion(x, y int16) {
	c.frame.mouseMotion(x, y)
}

func (c *client) NotifyMousePress(x, y int16) {
	c.frame.mousePress(x, y)
}

func (c *client) NotifyMouseRelease(x, y int16) {
	c.frame.mouseRelease(x, y)
}

func (c *client) NotifyMoveResizeEnded() {
	c.frame.notifyInnerGeometry()
}

func (c *client) NotifyUnFullscreen() {
	c.full = false
	c.frame.unmaximizeApply()
}

func (c *client) NotifyUnIconify() {
	c.newFrame()
	if c.frame == nil {
		return
	}

	c.iconic = false
	c.frame.show()
	c.frame.show()
	x11.WindowExtendedHintsRemove(c.wm.X(), c.win, "_NET_WM_STATE_HIDDEN")
}

func (c *client) NotifyUnMaximize() {
	c.maximized = false
	c.frame.unmaximizeApply()
	x11.WindowExtendedHintsRemove(c.wm.X(), c.win, "_NET_WM_STATE_MAXIMIZED_VERT")
	x11.WindowExtendedHintsRemove(c.wm.X(), c.win, "_NET_WM_STATE_MAXIMIZED_HORZ")
}

func (c *client) RaiseAbove(win fynedesk.Window) {
	screen := fynedesk.Instance().Screens().ScreenForWindow(c)
	topID := c.wm.WinIDForScreen(screen)
	if win != nil {
		topID = win.(*client).id
	}

	c.Focus()
	if c.id == topID {
		return
	}

	err := xproto.ConfigureWindowChecked(c.wm.Conn(), c.id, xproto.ConfigWindowSibling|xproto.ConfigWindowStackMode,
		[]uint32{uint32(topID), uint32(xproto.StackModeAbove)}).Check()
	if err != nil {
		fyne.LogError("Restack Error", err)
	}
}

func (c *client) RaiseToTop() {
	c.wm.RaiseToTop(c)
}

func (c *client) Refresh() {
	if c.frame == nil {
		return
	}

	c.frame.applyTheme(true)
}

func (c *client) SettingsChanged() {
	if c.frame == nil {
		return
	}

	c.frame.updateScale()
}

func (c *client) SizeMax() (int, int) {
	return windowSizeMax(c.wm.X(), c.ChildID())
}

func (c *client) SizeMin() (uint, uint) {
	return windowSizeMin(c.wm.X(), c.ChildID())
}

func (c *client) TopWindow() bool {
	if c.wm.TopWindow() == c {
		return true
	}
	return false
}

func (c *client) Unfullscreen() {
	c.fullscreenMessage(x11.WindowStateActionRemove)
	x11.WindowExtendedHintsRemove(c.wm.X(), c.win, "_NET_WM_STATE_FULLSCREEN")
}

func (c *client) Uniconify() {
	c.stateMessage(icccm.StateNormal)
	windowStateSet(c.wm.X(), c.win, icccm.StateNormal)
}

func (c *client) Unmaximize() {
	c.maximizeMessage(x11.WindowStateActionRemove)
}

func (c *client) fullscreenMessage(action x11.WindowStateAction) {
	ewmh.WmStateReq(c.wm.X(), c.win, int(action), "_NET_WM_STATE_FULLSCREEN")
}

func (c *client) getWindowGeometry() (int16, int16, uint16, uint16) {
	return c.frame.getGeometry()
}

func (c *client) maximizeMessage(action x11.WindowStateAction) {
	ewmh.WmStateReqExtra(c.wm.X(), c.win, int(action), "_NET_WM_STATE_MAXIMIZED_VERT",
		"_NET_WM_STATE_MAXIMIZED_HORZ", 1)
}

func (c *client) newFrame() {
	c.frame = newFrame(c)
}

func (c *client) stateMessage(state int) {
	stateChangeAtom, err := xprop.Atm(c.wm.X(), "WM_CHANGE_STATE")
	if err != nil {
		fyne.LogError("Error getting X Atom", err)
		return
	}
	cm, err := xevent.NewClientMessage(32, c.win, stateChangeAtom,
		state)
	if err != nil {
		fyne.LogError("Error creating client message", err)
		return
	}
	err = xevent.SendRootEvent(c.wm.X(), cm, xproto.EventMaskSubstructureNotify|xproto.EventMaskSubstructureRedirect)
}