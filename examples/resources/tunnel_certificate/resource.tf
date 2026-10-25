resource "anthropic_tunnel" "corp" {
  display_name = "corp-mcp"
}

# The public CA certificate that signs the proxy's server certificate. Only
# the certificate is registered; its private key stays with your PKI.
resource "anthropic_tunnel_certificate" "ca" {
  tunnel_id          = anthropic_tunnel.corp.id
  ca_certificate_pem = file("${path.module}/ca.crt")

  # On replacement, archive the old CA only once the API has accepted the new
  # one (a tunnel holds up to two certificates).
  lifecycle {
    create_before_destroy = true
  }
}

output "tunnel_certificate_fingerprint" {
  description = "SHA-256 fingerprint of the registered CA certificate."
  value       = anthropic_tunnel_certificate.ca.fingerprint
}

output "tunnel_certificate_expires_at" {
  description = "Expiry of the registered CA certificate."
  value       = anthropic_tunnel_certificate.ca.expires_at
}
