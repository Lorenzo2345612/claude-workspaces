package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var requestNativeBadge = func() error { return startNativeBadge(false) }
var requestNativeBadgeForWindow = func() error { return startNativeBadge(true) }

func startNativeBadge(force bool) error {
	if os.Getenv("WSL_DISTRO_NAME") == "" {
		return nil
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"_notification-badge"}
	if force {
		args = append(args, "force")
	}
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func cmdNotificationBadge(force bool) error {
	if os.Getenv("WSL_DISTRO_NAME") == "" {
		return nil
	}
	lock, err := usageLock("notification-badge.lock", false)
	if err != nil {
		return err
	}
	defer lock.Close()
	list, err := readNotifications()
	if err != nil {
		return err
	}
	var last struct {
		Count int
		At    int64
	}
	stamp := filepath.Join(runDir(), "notification-badge.json")
	if data, err := os.ReadFile(stamp); err == nil {
		_ = json.Unmarshal(data, &last)
		if !force && last.Count == len(list) && time.Now().Unix()-last.At < 300 {
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", powershellEncodedCommand(windowsBadgeScript(len(list), len(list) > last.Count)))
	cmd.Env = windowsSoundEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = writeFileAtomic(filepath.Join(runDir(), "notification-badge-error.log"), append([]byte(err.Error()+"\n"), output...))
		return err
	}
	if strings.TrimSpace(string(output)) == "0" {
		return nil
	}
	last.Count, last.At = len(list), time.Now().Unix()
	data, _ := json.Marshal(last)
	return writeFileAtomic(stamp, data)
}

func windowsBadgeScript(count int, flash bool) string {
	return `$ErrorActionPreference='Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -ReferencedAssemblies System.Drawing -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Drawing;
using System.Runtime.InteropServices;
using System.Text;
[ComImport,Guid("56FDF344-FD6D-11d0-958A-006097C9A090"),ClassInterface(ClassInterfaceType.None)] class CWTaskbar {}
[ComImport,Guid("EA1AFB91-9E28-4B86-90E9-9E9F8A5EEFAF"),InterfaceType(ComInterfaceType.InterfaceIsIUnknown)] interface ICWTaskbar {
 void HrInit();void AddTab(IntPtr h);void DeleteTab(IntPtr h);void ActivateTab(IntPtr h);void SetActiveAlt(IntPtr h);
 void MarkFullscreenWindow(IntPtr h,[MarshalAs(UnmanagedType.Bool)]bool f);
 void SetProgressValue(IntPtr h,ulong c,ulong t);void SetProgressState(IntPtr h,uint f);
 void RegisterTab(IntPtr h,IntPtr p);void UnregisterTab(IntPtr h);void SetTabOrder(IntPtr h,IntPtr b);void SetTabActive(IntPtr h,IntPtr p,uint f);
 void ThumbBarAddButtons(IntPtr h,uint n,IntPtr b);void ThumbBarUpdateButtons(IntPtr h,uint n,IntPtr b);void ThumbBarSetImageList(IntPtr h,IntPtr l);
 void SetOverlayIcon(IntPtr h,IntPtr icon,[MarshalAs(UnmanagedType.LPWStr)]string description);
}
public static class CWBadge {
 delegate bool EnumProc(IntPtr h,IntPtr p);
 [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc callback,IntPtr p);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
 [DllImport("user32.dll")] static extern bool DestroyIcon(IntPtr h);
 [StructLayout(LayoutKind.Sequential)] struct FLASH {public uint cbSize;public IntPtr hwnd;public uint flags;public uint count;public uint timeout;}
 [DllImport("user32.dll")] static extern bool FlashWindowEx(ref FLASH info);
 public static int Apply(int count,bool flash) {
  IntPtr icon=IntPtr.Zero;Bitmap bitmap=null;Graphics graphics=null;Font font=null;Brush red=null;
  try {
   if(count>0){bitmap=new Bitmap(32,32);graphics=Graphics.FromImage(bitmap);graphics.SmoothingMode=System.Drawing.Drawing2D.SmoothingMode.AntiAlias;red=new SolidBrush(Color.FromArgb(225,66,81));graphics.FillEllipse(red,0,0,31,31);font=new Font("Segoe UI",count>99?11:15,FontStyle.Bold,GraphicsUnit.Pixel);StringFormat format=new StringFormat();format.Alignment=StringAlignment.Center;format.LineAlignment=StringAlignment.Center;graphics.DrawString(count>99?"99+":count.ToString(),font,Brushes.White,new RectangleF(0,0,32,31),format);format.Dispose();icon=bitmap.GetHicon();}
   ICWTaskbar bar=(ICWTaskbar)new CWTaskbar();bar.HrInit();int updated=0;
   EnumWindows(delegate(IntPtr h,IntPtr p){var title=new StringBuilder(1024);GetWindowText(h,title,title.Capacity);if(IsWindowVisible(h)&&(title.ToString().IndexOf("Agent Workspaces",StringComparison.OrdinalIgnoreCase)>=0||title.ToString()=="cw"||title.ToString().StartsWith("cw · "))){try{bar.SetOverlayIcon(h,icon,count+" avisos pendientes");updated++;}catch(COMException){}if(flash&&count>0&&GetForegroundWindow()!=h){FLASH info=new FLASH();info.cbSize=(uint)Marshal.SizeOf(typeof(FLASH));info.hwnd=h;info.flags=2;info.count=3;FlashWindowEx(ref info);}}return true;},IntPtr.Zero);
   Marshal.ReleaseComObject(bar);return updated;
  } finally {if(icon!=IntPtr.Zero)DestroyIcon(icon);if(font!=null)font.Dispose();if(red!=null)red.Dispose();if(graphics!=null)graphics.Dispose();if(bitmap!=null)bitmap.Dispose();}
 }
}
'@
[CWBadge]::Apply(` + strconv.Itoa(count) + `,` + map[bool]string{true: "$true", false: "$false"}[flash] + `)
`
}
