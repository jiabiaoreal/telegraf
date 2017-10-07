PREFIX := /usr/local
VERSION := $(shell git describe --exact-match --tags 2>/dev/null)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
COMMIT := $(shell git rev-parse --short HEAD)
ifdef GOBIN
PATH := $(GOBIN):$(PATH)
else
PATH := $(subst :,/bin:,$(GOPATH))/bin:$(PATH)
endif

# Standard Telegraf build
default: prepare build
# Standard Telegraf build
default: build
TELEGRAF := telegraf$(shell go tool dist env | grep -q 'GOOS=.windows.' && echo .exe)

LDFLAGS := $(LDFLAGS) -X main.commit=$(COMMIT) -X main.branch=$(BRANCH)
ifdef VERSION
	LDFLAGS += -X main.version=$(VERSION)
endif


all:
	$(MAKE) telegraf

telegraf:
	go build -i -o $(TELEGRAF) -ldflags "$(LDFLAGS)" ./cmd/telegraf/telegraf.go

go-install:
	go install -ldflags "-w -s $(LDFLAGS)" ./cmd/telegraf

install: telegraf
	mkdir -p $(DESTDIR)$(PREFIX)/bin/
	cp $(TELEGRAF) $(DESTDIR)$(PREFIX)/bin/

test:
	go test -short ./...

deploy: telegraf
	gzip < telegraf > telegraf-${COMMIT}.gz
	mv telegraf-${COMMIT}.gz /var/soft/telegraf/
	ln -sf telegraf-${COMMIT}.gz /var/soft/telegraf/last.gz
	echo -n "version=" > /var/soft/telegraf/last.spec
	./telegraf --version >> /var/soft/telegraf/last.spec
	echo -n "sha256=" >> /var/soft/telegraf/last.spec
	sha256sum telegraf | sed 's/ .*//' >> /var/soft/telegraf/last.spec


lint:
	go vet ./...

test-all: lint
	go test ./...

package:
	./scripts/build.py --package --platform=all --arch=all
clean:
	-rm -f telegraf
	-rm -f telegraf.exe


.PHONY: telegraf install test lint test-all \
	package clean
