terraform {
  required_version = ">= 1.5.0"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
  }
}

provider "cloudflare" {
  # Credentials are supplied via the CLOUDFLARE_API_TOKEN environment variable.
  # The token must carry the "Access: Apps and Policies — Edit" permission.
  # See README.md for the full required scope.
}
