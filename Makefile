UNAME_S := $(shell uname -s 2>/dev/null)
ifeq ($(UNAME_S),Linux)
    OS = Linux
endif

CPU_OSARCH = "linux/arm64 linux/arm linux/amd64 linux/ppc64le linux/mips64 linux/390x linux/386 darwin/arm64 darwin/amd64"
CPUD_OSARCH = "linux/arm64 linux/arm linux/amd64 linux/ppc64le linux/mips64 linux/390x linux/386"

ifeq (, $(shell which cargo))
$(warn "No cargo in $(PATH), install rust if you want to build with it")
endif

ifeq (, $(shell which go))
$(warn "No go in $(PATH), install go if you want to build with it")
endif

ifeq (, $(shell which gox))
$(warn "No gox in $(PATH), install gox if you want to cross-compile with it")
endif

ifeq (, $(shell which docker))
$(warn "No docker in $(PATH), install docker if you want to create images")
endif

.PHONY: all go gox docker rust

all: go gox docker rust

go:
	mkdir -p bin
	-CGO_ENABLED=0 go build -o bin/cpu ./cmds/cpu/.
	-CGO_ENABLED=0 go build -o bin/decpu ./cmds/decpu/.
ifeq ($(OS),Linux)
	-CGO_ENABLED=0 go build -o bin/cpud ./cmds/cpud/.
	-CGO_ENABLED=0 go build -o bin/decpud ./cmds/decpud/.
endif

gox:
	mkdir -p bin
	-(cd bin && CGO_ENABLED=0 gox -osarch=$(CPU_OSARCH) ../cmds/cpu/.)
	-(cd bin && CGO_ENABLED=0 gox -osarch=$(CPUD_OSARCH) ../cmds/cpud/.)
	-(cd bin && CGO_ENABLED=0 gox -osarch=$(CPU_OSARCH) ../cmds/decpu/.)
	-(cd bin && CGO_ENABLED=0 gox -osarch=$(CPUD_OSARCH) ../cmds/decpud/.)

test:
	go test ./...

docker:
	-docker build . -t cpud:latest
	-IMAGE=cpud:latest ./TESTDOCKERCPU

rust:
	-cargo build --release

rust-format:
	-cargo fmt


clean:
	rm -rf bin
	rm -rf target
