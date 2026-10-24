// Disposable, publicly known test-only TLS identities. Never use these keys in a deployment.
import { X509Certificate, createHash } from "node:crypto";
import type { TLSMetadata } from "../src/inboundTLS";
export const firstTLS = {
  "certificate_pem": "-----BEGIN CERTIFICATE-----\nMIIBujCCAWCgAwIBAgIUNzWUPNdpYLZuwJcGjBoC31gnZVMwCgYIKoZIzj0EAwIw\nGzEZMBcGA1UEAwwQZWRnZS5leGFtcGxlLm9yZzAeFw0yNjEwMjQxNjIzMzhaFw0z\nNjEwMjExNjIzMzhaMBsxGTAXBgNVBAMMEGVkZ2UuZXhhbXBsZS5vcmcwWTATBgcq\nhkjOPQIBBggqhkjOPQMBBwNCAAT3uhR1T1V+5cYc8tNQGVnq9f5osUL7lhjSqzJt\n56k9cT+R2WSuAiOVl8YdpSI/VBSlMKXrNrjF9atji5MIIL2Fo4GBMH8wHQYDVR0O\nBBYEFBTPEDwhygZu4Vh8NQxdcoJyG4+BMB8GA1UdIwQYMBaAFBTPEDwhygZu4Vh8\nNQxdcoJyG4+BMA8GA1UdEwEB/wQFMAMBAf8wLAYDVR0RBCUwI4IQZWRnZS5leGFt\ncGxlLm9yZ4IPY2RuLmV4YW1wbGUub3JnMAoGCCqGSM49BAMCA0gAMEUCIG11fbYq\nHz4hymgLAFVpPXb44QAaJWpWmBuLPzFzd8ydAiEAiKuKjOWDPqGPmtdnNKMwpCfC\nElOzvx3FJZG5cGTcM/0=\n-----END CERTIFICATE-----\n",
  "private_key_pem": "-----BEGIN PRIVATE KEY-----\nMIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgVTddAlDkx8OKNXE/\nIz6udq4wo4ByVt+5+L3o8+Mu+UOhRANCAAT3uhR1T1V+5cYc8tNQGVnq9f5osUL7\nlhjSqzJt56k9cT+R2WSuAiOVl8YdpSI/VBSlMKXrNrjF9atji5MIIL2F\n-----END PRIVATE KEY-----\n"
};
export const renewedTLS = {
  "certificate_pem": "-----BEGIN CERTIFICATE-----\nMIIBuzCCAWCgAwIBAgIUCTFRyMkDvF2m9/YO4wSkdqY2RpcwCgYIKoZIzj0EAwIw\nGzEZMBcGA1UEAwwQZWRnZS5leGFtcGxlLm9yZzAeFw0yNjEwMjQxNjIzMzhaFw0z\nNjEwMjExNjIzMzhaMBsxGTAXBgNVBAMMEGVkZ2UuZXhhbXBsZS5vcmcwWTATBgcq\nhkjOPQIBBggqhkjOPQMBBwNCAAQeVUBBF3A1+afPwLhYNf3ppKfUFVFMqtcfhbMl\nky8fhccu3S3UHujsUqsjhiBoZwsGmQc4Z8wbViaerQiAHBVao4GBMH8wHQYDVR0O\nBBYEFFdX4+TDZfeqUWixkkILnM7pCGA7MB8GA1UdIwQYMBaAFFdX4+TDZfeqUWix\nkkILnM7pCGA7MA8GA1UdEwEB/wQFMAMBAf8wLAYDVR0RBCUwI4IQZWRnZS5leGFt\ncGxlLm9yZ4IPY2RuLmV4YW1wbGUub3JnMAoGCCqGSM49BAMCA0kAMEYCIQCEXvEH\n1IvtIz+A/JZq/lSdpIxYBJVHOxDC8dBd//YspgIhALIdt3YgqqlV23m3EWvFTsvJ\nBbmPuErOaIHB++Nb4MlW\n-----END CERTIFICATE-----\n",
  "private_key_pem": "-----BEGIN PRIVATE KEY-----\nMIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgPlTqSgEOMG5bwILj\nJSCBAIj2tB/6Duxaas7sDmgG6xGhRANCAAQeVUBBF3A1+afPwLhYNf3ppKfUFVFM\nqtcfhbMlky8fhccu3S3UHujsUqsjhiBoZwsGmQc4Z8wbViaerQiAHBVa\n-----END PRIVATE KEY-----\n"
};
export function certificateMetadata(pem: string): TLSMetadata {
  const cert = new X509Certificate(pem);
  return { sha256: createHash("sha256").update(cert.raw).digest("hex"), public_key_sha256: createHash("sha256").update(cert.publicKey.export({ type: "spki", format: "der" })).digest("hex"),
    dns_names: (cert.subjectAltName ?? "").split(", ").map(name => name.replace(/^DNS:/, "")), not_before_unix: Date.parse(cert.validFrom) / 1000, not_after_unix: Date.parse(cert.validTo) / 1000, key_type: "ECDSA" };
}
