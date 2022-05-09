FROM scratch

COPY cpud /bin/cpud
COPY cpu /bin/cpu
COPY date /bin
COPY cat /bin
COPY lib64 /lib64
COPY root /root

# Export necessary port
EXPOSE 23

# Command to run
ENTRYPOINT ["/bin/cpud"]
