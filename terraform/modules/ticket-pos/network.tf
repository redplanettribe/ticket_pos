# The VPC exists for one reason: Cloud SQL has no public IP, so it needs a
# private address, and a private address needs a network to live in.
#
# A private-IP Cloud SQL instance is not created *in* this network. It is
# created in a Google-managed producer network that is VPC-peered to ours, and
# the peering needs an IP range on our side reserved for it up front. That is
# "private services access": reserve a range, then peer it. Both steps below.
#
# Private services access needs no subnet of its own. The one subnet below
# exists for Cloud Run's direct VPC egress.
resource "google_compute_network" "main" {
  project = var.project_id
  name    = "${var.environment}-ticket-pos"

  # Subnets are declared explicitly; auto mode would create one in every region.
  auto_create_subnetworks = false
  description             = "Ticket POS ${var.environment} network"
}

# The block handed to Google's service producers. It must not overlap anything
# we might later place in this VPC, which is why it sits high in 10.0.0.0/8.
# /24 is 256 addresses: far more Cloud SQL instances than this project will hold,
# and the range cannot be shrunk after peering without recreating the peering.
resource "google_compute_global_address" "private_services" {
  project       = var.project_id
  name          = "${var.environment}-ticket-pos-private-services"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  address       = "10.240.0.0"
  prefix_length = 24
  network       = google_compute_network.main.id
}

resource "google_service_networking_connection" "private_services" {
  network                 = google_compute_network.main.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_services.name]

  # Tearing the peering down while a Cloud SQL instance still hangs off it
  # leaves the connection in a state Terraform cannot resolve. ABANDON removes
  # it from state and leaves the peering to be cleaned up after the instance.
  deletion_policy = "ABANDON"
}

# The subnet Cloud Run attaches to for *direct VPC egress*, the mechanism that
# lets a Cloud Run instance open a socket to the database's private IP.
#
# Direct VPC egress, not a Serverless VPC Access connector: a connector is a
# managed instance group of at least two e2-micro VMs that bill continuously
# whether or not anything is running, roughly $10/month against a deployment
# whose entire budget is ~$30. Direct egress attaches the instance to this
# subnet itself and costs nothing.
#
# The price is address consumption: every running instance holds an address from
# this range for its lifetime, and scaling churn holds a few beyond that. A /24
# leaves ~250 usable, far above the instance ceilings configured here, and a
# subnet range cannot be narrowed after creation.
#
# It must not overlap 10.240.0.0/24, which is reserved for the Cloud SQL peering.
resource "google_compute_subnetwork" "cloud_run" {
  project = var.project_id
  name    = "${var.environment}-ticket-pos-run"
  region  = var.region
  network = google_compute_network.main.id

  ip_cidr_range = var.cloud_run_subnet_cidr

  # Reaching *.googleapis.com — Secret Manager at startup, storage.googleapis.com
  # for presigned uploads — over Google's internal network rather than a public
  # address. Egress is PRIVATE_RANGES_ONLY, so in the normal case that traffic
  # never enters this subnet at all; this is what keeps the service working if
  # egress is ever widened, without also needing the Cloud NAT ADR 0007 declined.
  private_ip_google_access = true

  description = "Direct VPC egress for Cloud Run in ${var.environment}"
}
