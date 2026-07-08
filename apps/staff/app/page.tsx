import Link from "next/link";

export default function StaffDashboardPage() {
  return (
    <main>
      <h1>Staff Dashboard</h1>
      <p>Catalog management, POS mode, and sale imports will live here.</p>
      <nav>
        <Link href="/login">Sign in</Link>
      </nav>
    </main>
  );
}
