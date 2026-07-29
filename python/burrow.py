#!/usr/bin/env python3
import gi
gi.require_version('Gtk', '3.0')
gi.require_version('AyatanaAppIndicator3', '0.1')
from gi.repository import Gtk, GLib, AyatanaAppIndicator3 as AppIndicator3
import dbus
import dbus.mainloop.glib
import json
import os
import subprocess
import shutil

CONFIG_PATH = os.path.expanduser('~/.config/burrow/config.json')
_DIR = os.path.dirname(os.path.abspath(__file__))
ICONS = {
    'connected':    os.path.join(_DIR, 'icons', 'burrow.svg'),
    'disconnected': os.path.join(_DIR, 'icons', 'burrow.svg'),
    'trusted':      os.path.join(_DIR, 'icons', 'burrow.svg'),
    'error':        os.path.join(_DIR, 'icons', 'burrow.svg'),
}

def load_config():
    if os.path.exists(CONFIG_PATH):
        with open(CONFIG_PATH) as f:
            return json.load(f)
    return {'trusted_ssids': [], 'wg_connection': None, 'auto_connect': True}

def save_config(cfg):
    os.makedirs(os.path.dirname(CONFIG_PATH), exist_ok=True)
    with open(CONFIG_PATH, 'w') as f:
        json.dump(cfg, f, indent=2)

def nmcli(*args):
    result = subprocess.run(['nmcli', '--terse', *args], capture_output=True, text=True)
    return result.stdout.strip(), result.returncode

def get_current_ssid():
    out, code = nmcli('-f', 'active,ssid', 'dev', 'wifi')
    for line in out.splitlines():
        if line.startswith('yes:'):
            return line.split(':', 1)[1]
    return None

def get_wg_status(connection_name):
    if not connection_name:
        return False
    out, _ = nmcli('-f', 'name,state', 'con', 'show', '--active')
    for line in out.splitlines():
        parts = line.split(':')
        if len(parts) >= 2 and parts[0] == connection_name:
            return True
    return False

def wg_up(connection_name):
    _, code = nmcli('con', 'up', connection_name)
    return code == 0

def wg_down(connection_name):
    _, code = nmcli('con', 'down', connection_name)
    return code == 0

def import_wg_config(path, connection_name):
    _, code = nmcli('con', 'import', 'type', 'wireguard', 'file', path)
    if code != 0:
        return False, 'Import failed'
    # rename to friendly name if different
    basename = os.path.splitext(os.path.basename(path))[0]
    if basename != connection_name and connection_name:
        nmcli('con', 'modify', basename, 'connection.id', connection_name)
        return True, connection_name
    return True, basename

def list_wg_connections():
    out, _ = nmcli('-f', 'name,type', 'con', 'show')
    return [line.split(':')[0] for line in out.splitlines()
            if 'wireguard' in line.lower()]


class SettingsWindow(Gtk.Window):
    def __init__(self, app):
        super().__init__(title='Burrow — Settings')
        self.app = app
        self.cfg = app.cfg
        self.set_default_size(480, 400)
        self.set_border_width(16)
        self.connect('delete-event', lambda *_: self.hide() or True)

        root = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=16)
        self.add(root)

        # WireGuard connection
        wg_frame = Gtk.Frame(label=' WireGuard Connection ')
        wg_box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=8)
        wg_box.set_border_width(10)
        wg_frame.add(wg_box)

        conn_row = Gtk.Box(spacing=8)
        self.conn_combo = Gtk.ComboBoxText()
        self.conn_combo.set_hexpand(True)
        self._refresh_connections()
        self.conn_combo.connect('changed', self._on_conn_changed)
        conn_row.pack_start(Gtk.Label(label='Connection:'), False, False, 0)
        conn_row.pack_start(self.conn_combo, True, True, 0)
        wg_box.pack_start(conn_row, False, False, 0)

        import_row = Gtk.Box(spacing=8)
        import_btn = Gtk.Button(label='Import .conf file…')
        import_btn.connect('clicked', self._on_import)
        import_row.pack_start(import_btn, False, False, 0)
        self.import_status = Gtk.Label(label='')
        import_row.pack_start(self.import_status, False, False, 0)
        wg_box.pack_start(import_row, False, False, 0)

        root.pack_start(wg_frame, False, False, 0)

        # Trusted SSIDs
        ssid_frame = Gtk.Frame(label=' Trusted WiFi Networks ')
        ssid_box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=8)
        ssid_box.set_border_width(10)
        ssid_frame.add(ssid_box)

        self.ssid_store = Gtk.ListStore(str)
        for s in self.cfg.get('trusted_ssids', []):
            self.ssid_store.append([s])

        tv = Gtk.TreeView(model=self.ssid_store)
        tv.set_headers_visible(False)
        renderer = Gtk.CellRendererText()
        renderer.set_property('editable', True)
        renderer.connect('edited', self._on_ssid_edited)
        col = Gtk.TreeViewColumn('SSID', renderer, text=0)
        tv.append_column(col)
        self.tv = tv

        scroll = Gtk.ScrolledWindow()
        scroll.set_min_content_height(120)
        scroll.set_policy(Gtk.PolicyType.NEVER, Gtk.PolicyType.AUTOMATIC)
        scroll.add(tv)
        ssid_box.pack_start(scroll, True, True, 0)

        btn_row = Gtk.Box(spacing=8)
        add_current_btn = Gtk.Button(label='Add Current Network')
        add_current_btn.connect('clicked', self._on_add_current)
        add_btn = Gtk.Button(label='Add…')
        add_btn.connect('clicked', self._on_add_ssid)
        remove_btn = Gtk.Button(label='Remove')
        remove_btn.connect('clicked', self._on_remove_ssid)
        btn_row.pack_start(add_current_btn, False, False, 0)
        btn_row.pack_start(add_btn, False, False, 0)
        btn_row.pack_start(remove_btn, False, False, 0)
        ssid_box.pack_start(btn_row, False, False, 0)

        root.pack_start(ssid_frame, True, True, 0)

        # Auto-connect toggle
        auto_row = Gtk.Box(spacing=8)
        self.auto_switch = Gtk.Switch()
        self.auto_switch.set_active(self.cfg.get('auto_connect', True))
        self.auto_switch.connect('notify::active', self._on_auto_toggle)
        auto_row.pack_start(Gtk.Label(label='Auto-connect when on untrusted network'), False, False, 0)
        auto_row.pack_end(self.auto_switch, False, False, 0)
        root.pack_start(auto_row, False, False, 0)

        # Status bar
        self.status_label = Gtk.Label(label='')
        self.status_label.set_xalign(0)
        root.pack_start(self.status_label, False, False, 0)

        self.show_all()

    def _refresh_connections(self):
        self.conn_combo.remove_all()
        for name in list_wg_connections():
            self.conn_combo.append_text(name)
        current = self.cfg.get('wg_connection')
        if current:
            model = self.conn_combo.get_model()
            for i, row in enumerate(model):
                if row[0] == current:
                    self.conn_combo.set_active(i)
                    break

    def _on_conn_changed(self, combo):
        self.cfg['wg_connection'] = combo.get_active_text()
        save_config(self.cfg)
        self.app.update_status()

    def _on_import(self, _):
        dialog = Gtk.FileChooserDialog(
            title='Select WireGuard .conf file',
            parent=self,
            action=Gtk.FileChooserAction.OPEN,
        )
        dialog.add_buttons(Gtk.STOCK_CANCEL, Gtk.ResponseType.CANCEL,
                           Gtk.STOCK_OPEN, Gtk.ResponseType.OK)
        f = Gtk.FileFilter()
        f.set_name('WireGuard config (*.conf)')
        f.add_pattern('*.conf')
        dialog.add_filter(f)

        if dialog.run() == Gtk.ResponseType.OK:
            path = dialog.get_filename()
            dialog.destroy()
            ok, name = import_wg_config(path, None)
            if ok:
                self.cfg['wg_connection'] = name
                save_config(self.cfg)
                self._refresh_connections()
                self.import_status.set_text(f'Imported: {name}')
                self.app.update_status()
            else:
                self.import_status.set_text(f'Error: {name}')
        else:
            dialog.destroy()

    def _on_add_current(self, _):
        ssid = get_current_ssid()
        if ssid:
            self._add_ssid(ssid)
        else:
            self.status_label.set_text('No WiFi network currently connected.')

    def _on_add_ssid(self, _):
        dialog = Gtk.Dialog(title='Add Trusted Network', parent=self)
        dialog.add_buttons(Gtk.STOCK_CANCEL, Gtk.ResponseType.CANCEL,
                           Gtk.STOCK_OK, Gtk.ResponseType.OK)
        entry = Gtk.Entry()
        entry.set_placeholder_text('Network name (SSID)')
        entry.set_margin_start(12)
        entry.set_margin_end(12)
        entry.set_margin_top(12)
        entry.set_margin_bottom(12)
        dialog.get_content_area().add(entry)
        dialog.show_all()
        if dialog.run() == Gtk.ResponseType.OK:
            self._add_ssid(entry.get_text().strip())
        dialog.destroy()

    def _add_ssid(self, ssid):
        if not ssid:
            return
        existing = [r[0] for r in self.ssid_store]
        if ssid not in existing:
            self.ssid_store.append([ssid])
            self.cfg['trusted_ssids'] = [r[0] for r in self.ssid_store]
            save_config(self.cfg)
            self.app.update_status()

    def _on_remove_ssid(self, _):
        sel = self.tv.get_selection()
        model, it = sel.get_selected()
        if it:
            model.remove(it)
            self.cfg['trusted_ssids'] = [r[0] for r in self.ssid_store]
            save_config(self.cfg)
            self.app.update_status()

    def _on_ssid_edited(self, renderer, path, new_text):
        self.ssid_store[path][0] = new_text
        self.cfg['trusted_ssids'] = [r[0] for r in self.ssid_store]
        save_config(self.cfg)

    def _on_auto_toggle(self, switch, _):
        self.cfg['auto_connect'] = switch.get_active()
        save_config(self.cfg)
        self.app.update_status()


class WgTrustedApp:
    def __init__(self):
        dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)
        self.cfg = load_config()
        self.settings_win = None
        self._build_indicator()
        self._subscribe_nm()
        self.update_status()

    def _build_indicator(self):
        self.indicator = AppIndicator3.Indicator.new(
            'burrow', ICONS['disconnected'],
            AppIndicator3.IndicatorCategory.SYSTEM_SERVICES
        )
        self.indicator.set_title('Burrow')
        self.indicator.set_status(AppIndicator3.IndicatorStatus.ACTIVE)
        self.menu = Gtk.Menu()

        self.status_item = Gtk.MenuItem(label='Status: checking…')
        self.status_item.set_sensitive(False)
        self.menu.append(self.status_item)
        self.menu.append(Gtk.SeparatorMenuItem())

        self.connect_item = Gtk.MenuItem(label='Connect VPN')
        self.connect_item.connect('activate', lambda _: self._manual_connect())
        self.menu.append(self.connect_item)

        self.disconnect_item = Gtk.MenuItem(label='Disconnect VPN')
        self.disconnect_item.connect('activate', lambda _: self._manual_disconnect())
        self.menu.append(self.disconnect_item)

        self.menu.append(Gtk.SeparatorMenuItem())

        settings_item = Gtk.MenuItem(label='Settings…')
        settings_item.connect('activate', lambda _: self._open_settings())
        self.menu.append(settings_item)

        quit_item = Gtk.MenuItem(label='Quit')
        quit_item.connect('activate', lambda _: Gtk.main_quit())
        self.menu.append(quit_item)

        self.menu.show_all()
        self.indicator.set_menu(self.menu)

    def _subscribe_nm(self):
        bus = dbus.SystemBus()
        bus.add_signal_receiver(
            self._on_nm_state_changed,
            dbus_interface='org.freedesktop.NetworkManager',
            signal_name='StateChanged'
        )

    def _on_nm_state_changed(self, state):
        # NM state 70 = connected globally, 60 = connected site, 50 = connecting
        GLib.idle_add(self.update_status)

    def update_status(self):
        conn = self.cfg.get('wg_connection')
        ssid = get_current_ssid()
        trusted = ssid in self.cfg.get('trusted_ssids', []) if ssid else False
        connected = get_wg_status(conn)
        auto = self.cfg.get('auto_connect', True)

        if not conn:
            self.status_item.set_label('Status: no VPN configured')
            self.indicator.set_icon_full(ICONS['error'], 'No VPN configured')
        elif connected:
            self.status_item.set_label(f'Status: VPN on ({conn})')
            self.indicator.set_icon_full(ICONS['connected'], 'VPN connected')
        elif trusted:
            self.status_item.set_label(f'Status: trusted network ({ssid})')
            self.indicator.set_icon_full(ICONS['trusted'], 'Trusted network')
        elif ssid:
            self.status_item.set_label(f'Status: untrusted ({ssid}) — VPN off')
            self.indicator.set_icon_full(ICONS['disconnected'], 'Untrusted network')
        else:
            self.status_item.set_label('Status: no WiFi')
            self.indicator.set_icon_full(ICONS['disconnected'], 'No WiFi')

        # Auto-connect logic
        if auto and conn and ssid:
            if not trusted and not connected:
                wg_up(conn)
                GLib.timeout_add(1000, self.update_status)
            elif trusted and connected:
                wg_down(conn)
                GLib.timeout_add(1000, self.update_status)

        self.connect_item.set_sensitive(bool(conn) and not connected)
        self.disconnect_item.set_sensitive(bool(conn) and connected)
        return False

    def _manual_connect(self):
        conn = self.cfg.get('wg_connection')
        if conn:
            wg_up(conn)
            GLib.timeout_add(1500, self.update_status)

    def _manual_disconnect(self):
        conn = self.cfg.get('wg_connection')
        if conn:
            wg_down(conn)
            GLib.timeout_add(1500, self.update_status)

    def _open_settings(self):
        if self.settings_win is None:
            self.settings_win = SettingsWindow(self)
            self.settings_win.connect('destroy', lambda _: setattr(self, 'settings_win', None))
        else:
            self.settings_win.present()

    def run(self):
        Gtk.main()


if __name__ == '__main__':
    if not shutil.which('nmcli'):
        print('nmcli not found — NetworkManager is required')
        raise SystemExit(1)
    WgTrustedApp().run()
