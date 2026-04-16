package bridge

import "log"

// Tray functions are no-ops in server mode (no desktop environment).

func (a *App) UpdateTray(tray TrayContent) {
	log.Printf("UpdateTray: no-op in server mode")
}

func (a *App) UpdateTrayMenus(menus []MenuItem) {
	log.Printf("UpdateTrayMenus: no-op in server mode")
}

func (a *App) UpdateTrayAndMenus(tray TrayContent, menus []MenuItem) {
	log.Printf("UpdateTrayAndMenus: no-op in server mode")
}
