# The VPC exists for one reason: Cloud SQL has no public IP, so it needs a
# private address, and a private address needs a network to live in.
#
# A private-IP Cloud SQL instance is not created *in* this network. It is
# created in a Google-managed producer network that is VPC-peered to ours, and
# the peering needs an IP range on our side reserved for it up front. That is
# "private services access": reserve a range, then peer it. Both steps below.
#
# No subnets are created here. Private services access needs none, and the
# subnet Cloud Run's direct VPC egress will attach to belongs with Cloud Run (#47).
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
