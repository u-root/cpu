UNAME_S := $(shell uname -s 2>/dev/null)
ifeq ($(UNAME_S),Linux)
    OS = Linux
endif

CPU_OSARCH = "linux/arm64 linux/arm linux/amd64 linux/ppc64le linux/mips64 linux/390x linux/386 darwin/arm64 darwin/amd64"
CPUD_OSARCH = "linux/arm64 linux/arm linux/amd64 linux/ppc64le linux/mips64 linux/390x linux/386"

.PHONY: all go gox docker rust

all: go gox docker rust

go:
ifeq (, $(shell which go))
$(info No go in PATH, install go if you want to build with it)
else
	mkdir -p bin
	-CGO_ENABLED=0 go build -o bin/cpu ./cmds/cpu/.
	-CGO_ENABLED=0 go build -o bin/decpu ./cmds/decpu/.
ifeq ($(OS),Linux)
	-CGO_ENABLED=0 go build -o bin/cpud ./cmds/cpud/.
	-CGO_ENABLED=0 go build -o bin/decpud ./cmds/decpud/.
endif
endif

gox:
ifeq (, $(shell which gox))
$(info No gox in PATH, install go if you want to build with it)
else
	mkdir -p bin
	-(cd bin && CGO_ENABLED=0 gox -osarch=$(CPU_OSARCH) ../cmds/cpu/.)
	-(cd bin && CGO_ENABLED=0 gox -osarch=$(CPUD_OSARCH) ../cmds/cpud/.)
	-(cd bin && CGO_ENABLED=0 gox -osarch=$(CPU_OSARCH) ../cmds/decpu/.)
	-(cd bin && CGO_ENABLED=0 gox -osarch=$(CPUD_OSARCH) ../cmds/decpud/.)
endif

test:
	go test ./...

docker:
ifeq (, $(shell which docker))
$(info No docker in PATH, install docker if you want to create images"
else
	-docker build . -t cpud:latest
	-IMAGE=cpud:latest ./TESTDOCKERCPU
endif

rust:
ifeq (, $(shell which cargo))
$(info No cargo in PATH, install rust if you want to build with it)
else
	-cargo build --release
endif

rust-format:
ifeq (, $(shell which cargo))
$(info No cargo in PATH, install rust if you want to build with it)
else
	-cargo fmt
endif


clean:
	rm -rf bin
	rm -rf target
