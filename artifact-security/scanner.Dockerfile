FROM docker.io/aquasec/trivy@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c AS source
FROM scratch
COPY --from=source /usr/local/bin/trivy /usr/local/bin/trivy
COPY --from=source /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV GODEBUG=http2client=0
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/trivy"]
