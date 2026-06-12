.PHONY: all install reload enable

DESTDIR ?=
UNITDIR ?= /etc/systemd/system

all:
	@echo "Targets: install reload enable"
	@echo "reload implies install"
	@echo "enable implies reload implies install"

install:
	install -D -m 0644 cpud.service $(DESTDIR)$(UNITDIR)/cpud.service

reload: install
	systemctl daemon-reload

enable: reload
	systemctl enable --now cpud.service
