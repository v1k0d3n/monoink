# Used by the Decky CLI / plugin store CI: the backend directory is mounted
# at /backend and everything written to /backend/out ships in the plugin's
# bin/ directory.
FROM docker.io/library/golang:1.26
WORKDIR /backend
ENTRYPOINT ["make", "build"]
