IMAGE=$(USER)/cpu:latest
all: cat date cpud cpu key.pub Dockerfile
	docker build . -t $(IMAGE)

key.pub:
	ssh-keygen -f key -t rsa  -P ""

cpud:
	CGO_ENABLED=0 go build -o cpud ../cmds/cpud/.

cpu:
	CGO_ENABLED=0 GOBIN=`pwd` go install  github.com/u-root/cpu/cmds/cpu

date:
	CGO_ENABLED=0 GOBIN=`pwd` go install  github.com/u-root/u-root/cmds/core/date

cat:
	CGO_ENABLED=0 GOBIN=`pwd` go install  github.com/u-root/u-root/cmds/core/cat

rundocker:
	-docker rm cpud_test
	docker run --mount type=bind,source=$(PWD)/key.pub,target=/key.pub --mount type=bind,source=$(PWD)/key,target=/key --name cpud_test --privileged=true -t -i -p 23:23 $(IMAGE)

run:
	@echo ===================================================
	@echo Check the date, is it `date`
	cpu -key key localhost date
	@echo ===================================================
	@echo Note the pwd -- it is `pwd`
	cpu -key key localhost pwd
	@echo ===================================================
	@echo Note that ls is the same local or remote
	cpu -key key localhost ls
	@echo ===================================================
	@echo note that hostname is not `hostname`
	cpu -key key localhost hostname
	@echo ===================================================
	@echo note the mounts ...
	cpu -key key localhost mount | grep 127.0.0.1
	@echo ===================================================
	@echo run with no 9p and no mounts
	@echo note that there is no homedir, so you must set PWD=/
	PWD=/ cpu -key key -9p=false -namespace="" localhost /bin/date
	@echo you can also prune the namespace
	@echo note that there is no homedir, so you must set PWD=/
	PWD=/ cpu -key key -9p=false -namespace="/home" localhost /bin/cat /proc/mounts | grep 127.0.0.1


cpuindocker:
	docker exec -it cpud_test /bin/cpu -key /key localhost /bin/date

push:
	docker push 
clean:
	rm -f cpu cpud key key.pub date
