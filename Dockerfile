FROM golang:latest as builder

COPY . /app/
WORKDIR /app
RUN mkdir -p /app/root
RUN mkdir -p /app/lib64 
RUN CGO_ENABLED=0 go build -o cpud ./cmds/cpud/.
RUN CGO_ENABLED=0 go build -o cpu ./cmds/cpu/.
RUN CGO_ENABLED=0 go build -o decpu ./cmds/decpu/.
RUN CGO_ENABLED=0 go build -o decpud ./cmds/decpud/.
RUN CGO_ENABLED=0 GOBIN=`pwd` go install  github.com/u-root/u-root/cmds/core/date
RUN CGO_ENABLED=0 GOBIN=`pwd` go install  github.com/u-root/u-root/cmds/core/cat

FROM scratch

COPY --from=builder /app/decpud /bin/decpud
COPY --from=builder /app/decpu /bin/decpu
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
