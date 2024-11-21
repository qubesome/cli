package mime

const (
	DefaultMimeHandler = `[Desktop Entry]
Version=1.0
Type=Application
Name=qubesome
Exec=/usr/local/bin/qubesome xdg-open %u
StartupNotify=false
`
	MimesList = `[Default Applications]
x-scheme-handler/slack=qubesome-default-handler.desktop;

application/x-yaml=qubesome-default-handler.desktop;
text/english=qubesome-default-handler.desktop;
text/html=qubesome-default-handler.desktop;
text/plain=qubesome-default-handler.desktop;
text/x-c=qubesome-default-handler.desktop;
text/x-c++=qubesome-default-handler.desktop;
text/x-makefile=qubesome-default-handler.desktop;
text/xml=qubesome-default-handler.desktop;
x-www-browser=qubesome-default-handler.desktop;

x-scheme-handler/http=qubesome-default-handler.desktop;
x-scheme-handler/https=qubesome-default-handler.desktop;
x-scheme-handler/about=qubesome-default-handler.desktop;
x-scheme-handler/unknown=qubesome-default-handler.desktop;

[Removed Associations]
x-scheme-handler/slack=slack.desktop;
x-scheme-handler/http=firefox.desktop;
x-scheme-handler/https=firefox.desktop;
x-scheme-handler/snap=snap-handle-link.desktop;
`
)
