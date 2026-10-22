FROM docker.io/aquasec/trivy@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969 AS source
FROM scratch
COPY --from=source /usr/local/bin/trivy /usr/local/bin/trivy
COPY scanner-ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV GODEBUG=http2client=0
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/trivy"]
