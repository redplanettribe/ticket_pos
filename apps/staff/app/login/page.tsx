export default function LoginPage() {
  return (
    <main>
      <h1>Sign in</h1>
      <p>Enter your email to receive a one-time passcode.</p>
      <form>
        <label htmlFor="email">Email</label>
        <input id="email" name="email" type="email" autoComplete="email" required />
        <button type="submit">Send code</button>
      </form>
    </main>
  );
}
