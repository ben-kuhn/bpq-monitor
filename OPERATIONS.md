# BPQ Monitor — Operations Reference

## Managing modem services

All modem services run as systemd **user** services.

### From the browser UI

Click any status chip in the top bar to open the action menu.  Enter the
action password and choose Start, Stop, or Restart.

### From the command line

```sh
systemctl --user status  <service>
systemctl --user start   <service>
systemctl --user stop    <service>
systemctl --user restart <service>
```

Example service names (configure your own in `bpq-monitor.toml`):

| Service name        | Modem            | BPQ port |
|---------------------|------------------|----------|
| `direwolf-vhf`      | Direwolf VHF     | 2        |
| `qtsoundmodem-hf`   | QtSoundModem HF  | 3        |
| `vara-fm`           | VARA FM          | 6        |
| `vara-hf`           | VARA HF          | 7        |
| `mercury`           | Mercury HF       | 8        |
| `bpq-monitor`       | Monitor app      | —        |

## Viewing logs

### From the browser UI

Click the **Log** button in the top-right corner of any monitor pane to
switch from BPQ traffic view to a live tail of that modem's journal.
Click **Traffic** to switch back.

### From the command line

```sh
# Follow a modem service log live
journalctl --user -u direwolf-vhf.service -f

# Show the last 100 lines
journalctl --user -u vara-fm.service -n 100

# Show bpq-monitor's own log (watchdog restarts appear here)
journalctl --user -u bpq-monitor.service -f
```

## Automatic watchdog

The monitor app watches every configured service and restarts it
automatically when:

- The service has been **unhealthy for more than 15 minutes** (not
  connected per BPQ port status, or not running per systemd).
- **QtSoundModem only** (`deaf_check = true` in `bpq-monitor.toml`):
  L2 frames heard has not changed for more than **1 hour**, indicating
  the audio decoder has stalled even though the port still shows
  "Connected to TNC".

Watchdog restart events are logged to `bpq-monitor.service`:

```sh
journalctl --user -u bpq-monitor.service -g watchdog
```

## Updating the monitor app

After changing source files, rebuild and install via your package manager.
With NixOS:

```sh
sudo nixos-rebuild switch
```

NixOS will rebuild and hot-reload `bpq-monitor.service` automatically.
