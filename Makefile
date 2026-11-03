# inua: Hyprland desktop shell in Go + GTK4 (docs/architecture.md).
#
#   make           build bin/inua-shell (the shell), bin/inua (command line),
#                  bin/inua-apps (light apps), bin/inua-files (the file
#                  manager), bin/inua-viewer (images, PDFs, Markdown and
#                  text), bin/inua-games (light games),
#                  bin/inua-game-lobby (the games' lobby on the internet),
#                  bin/inua-pinentry (gpg's prompt), bin/inua-askpass (ssh's,
#                  sudo's and git's), bin/inua-assistant (the
#                  assistant), bin/inua-keyring (the Secret Service kept in
#                  pass), bin/inua-term (the terminal), bin/inua-termd (its
#                  host), bin/inua-mail (mail) and bin/inua-share-picker
#                  (choosing what to share of the screen)
#   make term-engine
#                  build bin/libinua-vt.so, inua-term's engine (libghostty-vt
#                  at its pinned commit; needs git and Zig 0.16)
#                  (INUA_GOOGLE_CLIENT_SECRET=… make sets inua-mail's
#                  Google client secret; releases take it from CI)
#   make deps      install what inua needs to run, build and test
#                  (Ubuntu 26.04, sudo; the full list is in docs/design.md)
#   make deps-desktop
#                  install the companions the sample hyprland.conf starts
#                  and the tools behind the control center's buttons
#   make deps-dm   install the recommended greetd + tuigreet (Ubuntu 26.04)
#   make generate  recompile ui/*.blp to ui/*.ui (needs blueprint-compiler)
#   make test      go vet + unit tests
#   make nested    run the shell in a nested Hyprland window (from GNOME)
#   make run       run the shell in the current Hyprland session
#   make install   copy the binaries to $(PREFIX)/bin (default ~/.local/bin),
#                  and the light apps' launchers and icons
#   make install-config
#                  write the sample config into ~/.config with `inua init`
#                  (never overwrites existing files) and set gpg's pinentry
#   make install-fcitx
#                  copy the Fcitx 5 themes (inua-light, inua-dark) into
#                  ~/.local/share/fcitx5/themes; pick them in Fcitx's settings
#   make install-session
#                  install binaries, user config and a Wayland session entry
#                  (sudo only for the shared launcher and session entry)
#                  DM=greetd (default), gdm or sddm; never switches services

export CGO_ENABLED = 0

PREFIX ?= $(HOME)/.local
CONFIG_HOME ?= $(or $(XDG_CONFIG_HOME),$(HOME)/.config)
DM ?= greetd

.PHONY: all build term-engine deps deps-desktop deps-dm generate i18n test nested run install install-config install-fcitx install-session clean

all: build

build:
	go build -buildvcs=false -o bin/inua-shell ./cmd/inua-shell
	go build -buildvcs=false -o bin/inua ./cmd/inua
	go build -buildvcs=false -o bin/inua-apps ./cmd/inua-apps
	go build -buildvcs=false -o bin/inua-files ./cmd/inua-files
	go build -buildvcs=false -o bin/inua-viewer ./cmd/inua-viewer
	go build -buildvcs=false -o bin/inua-vm ./cmd/inua-vm
	go build -buildvcs=false -o bin/inua-remote ./cmd/inua-remote
	go build -buildvcs=false -o bin/inua-connect ./cmd/inua-connect
	go build -buildvcs=false -o bin/inua-disk-usage ./cmd/inua-disk-usage
	go build -buildvcs=false -ldflags "-X github.com/lyricat/inua/internal/apps/mail.GoogleClientSecret=$(INUA_GOOGLE_CLIENT_SECRET)" -o bin/inua-mail ./cmd/inua-mail
	go build -buildvcs=false -o bin/inua-games ./cmd/inua-games
	go build -buildvcs=false -o bin/inua-game-lobby ./cmd/inua-game-lobby
	go build -buildvcs=false -o bin/inua-pinentry ./cmd/inua-pinentry
	go build -buildvcs=false -o bin/inua-askpass ./cmd/inua-askpass
	go build -buildvcs=false -o bin/inua-share-picker ./cmd/inua-share-picker
	go build -buildvcs=false -o bin/inua-assistant ./cmd/inua-assistant
	go build -buildvcs=false -o bin/inua-keyring ./cmd/inua-keyring
	go build -buildvcs=false -o bin/inua-term ./cmd/inua-term
	go build -buildvcs=false -o bin/inua-termd ./cmd/inua-termd

term-engine:
	scripts/libghostty.sh

deps:
	sudo apt install -y hyprland libgtk-4-1 libgtk4-layer-shell0 \
		golang-go gcc dbus-daemon blueprint-compiler python3 pass gnupg \
		gstreamer1.0-plugins-base gstreamer1.0-plugins-good

deps-desktop:
	sudo apt install -y kitty udisks2 rclone fuse3 hypridle wf-recorder libturbojpeg0 libpoppler-glib8t64 \
		xdg-desktop-portal-hyprland xdg-desktop-portal-gtk \
		grim slurp wl-clipboard brightnessctl playerctl fcitx5 xdg-terminal-exec \
		pass gnupg fonts-ibm-plex fonts-noto-cjk \
		gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav

deps-dm:
	sudo apt install -y greetd tuigreet

generate:
	go generate ./ui

# Gather the text to translate into po/inua.pot and bring the catalogs in
# internal/i18n/locales up to date with it.
i18n:
	go run ./internal/i18n/extract

test:
	python3 scripts/test-session.py
	go vet ./...
	CGO_ENABLED=1 go test -race ./...

nested: build
	scripts/spike.sh shell nested

run: build
	bin/inua-shell

DATA_HOME ?= $(or $(XDG_DATA_HOME),$(HOME)/.local/share)

install: build
	install -Dm755 -t "$(PREFIX)/bin" bin/inua-shell bin/inua bin/inua-apps bin/inua-files bin/inua-viewer bin/inua-vm bin/inua-remote bin/inua-connect bin/inua-disk-usage bin/inua-mail bin/inua-games bin/inua-game-lobby bin/inua-pinentry bin/inua-askpass bin/inua-share-picker bin/inua-assistant bin/inua-keyring bin/inua-term bin/inua-termd
	# inua-term's engine, when built (make term-engine)
	if [ -f bin/libinua-vt.so ]; then install -Dm644 -t "$(PREFIX)/lib/inua" bin/libinua-vt.so; fi
	install -Dm644 -t "$(DATA_HOME)/applications" config/apps/*.desktop
	install -Dm644 -t "$(DATA_HOME)/icons/hicolor/scalable/apps" config/apps/*.svg
	install -Dm644 -t "$(DATA_HOME)/icons/hicolor/16x16/apps" config/apps/small/*.svg
	# inua's icon themes (design/icons), their aliases symlinks: the objects,
	# with inua's apps' icons copied in, and the soft and flat sets, which
	# have their own and link to the objects' 16 px drawings
	mkdir -p "$(DATA_HOME)/icons"
	for t in inua-objects inua-objects-soft inua-objects-flat; do rm -rf "$(DATA_HOME)/icons/$$t"; cp -a config/icons/$$t "$(DATA_HOME)/icons/$$t"; done
	install -m644 -t "$(DATA_HOME)/icons/inua-objects/scalable/apps" config/apps/*.svg
	install -m644 -t "$(DATA_HOME)/icons/inua-objects/16/apps" config/apps/small/*.svg
	-for t in inua-objects inua-objects-soft inua-objects-flat; do gtk-update-icon-cache -q -t "$(DATA_HOME)/icons/$$t" 2>/dev/null; done

install-config: build
	PATH="$(PREFIX)/bin:$$PATH" bin/inua init -dir "$(CONFIG_HOME)"

install-fcitx:
	mkdir -p "$(DATA_HOME)/fcitx5/themes"
	cp -r config/fcitx5/themes/* "$(DATA_HOME)/fcitx5/themes/"

install-session: install
	$(MAKE) install-config
	sudo sh scripts/install-session.sh "$(DM)"

clean:
	rm -rf bin
