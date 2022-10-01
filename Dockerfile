FROM golang:latest as builder

COPY . /app/
WORKDIR /app
RUN mkdir -p /app/root
RUN mkdir -p /app/lib64 
RUN CGO_ENABLED=0 go build -o cpud ./cmds/cpud/.
RUN CGO_ENABLED=0 go build -o cpu ./cmds/cpu/.
RUN CGO_ENABLED=0 go build -o dcpu ./cmds/dcpu/.
RUN CGO_ENABLED=0 go build -o dcpud ./cmds/dcpud/.
RUN CGO_ENABLED=0 GOBIN=`pwd` go install  github.com/u-root/u-root/cmds/core/date
RUN CGO_ENABLED=0 GOBIN=`pwd` go install  github.com/u-root/u-root/cmds/core/cat

FROM scratch

COPY --from=builder /app/dcpud /bin/dcpud
COPY --from=builder /app/dcpu /bin/dcpu
COPY --from=builder /app/cpud /bin/cpud
COPY --from=builder /app/cpu /bin/cpu
COPY --from=builder /app/date /bin
COPY --from=builder /app/cat /bin
COPY --from=builder /app/lib64 /lib64
COPY --from=builder /app/root /root

# Export necessary port
EXPOSE 17010

# Command to run
CMD ["/bin/cpud"]
