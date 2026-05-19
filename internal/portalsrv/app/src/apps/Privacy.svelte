<script lang="ts">
  let expandedSections: Set<string> = new Set();

  function toggleSection(section: string) {
    if (expandedSections.has(section)) {
      expandedSections.delete(section);
    } else {
      expandedSections.add(section);
    }
    expandedSections = expandedSections;
  }
</script>

<div class="legal-container">
  <div class="legal-content">
    <div class="legal-header">
      <a href="/" class="back-link">← Back Home</a>
      <h1>Privacy Policy</h1>
      <p>Last updated: January 2026</p>
    </div>

    <div class="sections">
      <section class="legal-section">
        <h2>1. Introduction</h2>
        <p>
          Wantastic ("Company," "we," "us," "our") is committed to protecting
          your privacy. This Privacy Policy explains how we collect, use,
          disclose, and safeguard your information when you visit our website
          and use our services.
        </p>
      </section>

      <section class="legal-section">
        <h2>2. Information We Collect</h2>
        <p>
          We collect information you provide directly to us, such as when you:
        </p>
        <ul>
          <li>Create an account (name, email, password)</li>
          <li>
            Use our services (WireGuard peer configurations, SSH sessions)
          </li>
          <li>Contact customer support</li>
          <li>Participate in surveys or feedback</li>
        </ul>
        <p>
          We also automatically collect certain information about your device
          and usage patterns:
        </p>
        <ul>
          <li>IP address and device information</li>
          <li>Log data and analytics</li>
          <li>Cookies and tracking technologies</li>
        </ul>
      </section>

      <section class="legal-section">
        <h2>3. How We Use Your Information</h2>
        <p>We use the information we collect to:</p>
        <ul>
          <li>Provide and maintain our services</li>
          <li>Send transactional emails and notifications</li>
          <li>Improve and optimize our platform</li>
          <li>Ensure security and prevent fraud</li>
          <li>Comply with legal obligations</li>
        </ul>
      </section>

      <section class="legal-section">
        <h2>4. Data Security</h2>
        <p>
          We implement industry-standard security measures including encryption,
          mTLS authentication, and secure key management. However, no method of
          transmission over the internet is 100% secure. We cannot guarantee
          absolute security of your data.
        </p>
      </section>

      <section class="legal-section">
        <h2>5. Third-Party Services</h2>
        <p>Our service integrates with third-party providers:</p>
        <ul>
          <li>
            <strong>Twilio:</strong> SMS verification. See their
            <a href="https://www.twilio.com/privacy" target="_blank"
              >privacy policy</a
            >
          </li>
          <li>
            <strong>Google Cloud:</strong> Infrastructure. See their
            <a href="https://cloud.google.com/privacy" target="_blank"
              >privacy policy</a
            >
          </li>
        </ul>
      </section>

      <section class="legal-section">
        <h2>6. Data Retention</h2>
        <p>
          We retain your data for as long as your account is active. Upon
          account deletion, we retain data as required by law or as necessary
          for business purposes. WireGuard logs are retained for 30 days for
          diagnostics purposes.
        </p>
      </section>

      <section class="legal-section">
        <h2>7. Your Rights</h2>
        <p>You have the right to:</p>
        <ul>
          <li>Access your personal data</li>
          <li>Correct inaccurate data</li>
          <li>Request deletion of your data</li>
          <li>Export your data (data portability)</li>
          <li>Opt-out of marketing communications</li>
        </ul>
        <p>To exercise these rights, contact us at privacy@wantastic.app</p>
      </section>

      <section class="legal-section">
        <h2>8. Cookies</h2>
        <p>
          We use cookies and similar tracking technologies to enhance your
          experience. You can control cookies through your browser settings.
        </p>
      </section>

      <section class="legal-section">
        <h2>9. Changes to This Policy</h2>
        <p>
          We may update this privacy policy from time to time. We will notify
          you of material changes via email or through our website.
        </p>
      </section>

      <section class="legal-section">
        <h2>10. Contact Us</h2>
        <p>For privacy-related questions or concerns, contact us at:</p>
        <p>Email: privacy@wantastic.app</p>
        <p>Address: Available upon request</p>
      </section>
    </div>
  </div>
</div>

<style>
  .legal-container {
    min-height: 100dvh;
    background: var(--mica);
    padding: 40px 20px;
    overflow-y: auto;
    display: flex;
    justify-content: center;
    align-items: flex-start;
    font-family: "Segoe UI Variable", "Segoe UI", system-ui, -apple-system,
      sans-serif;
  }

  @media (prefers-color-scheme: dark) {
    .legal-container {
      background: var(--mica);
    }
  }

  .legal-content {
    max-width: 800px;
    width: 100%;
    height: fit-content;
    padding: 48px 24px;
    margin-bottom: 40px;
    animation: fadeIn 0.8s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
      transform: translateY(10px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .legal-header {
    margin-bottom: 40px;
    text-align: center;
  }

  .back-link {
    color: rgb(var(--clrPrm));
    text-decoration: none;
    font-size: 14px;
    margin-bottom: 24px;
    display: inline-flex;
    align-items: center;
    transition: all 0.2s;
    font-weight: 600;
    padding: 8px 16px;
    background: rgb(var(--clr) / 5%);
    border-radius: 20px;
  }

  .back-link:hover {
    background: rgb(var(--clr) / 10%);
    transform: translateY(-1px);
  }

  .legal-header h1 {
    margin: 12px 0 8px 0;
    font-size: 32px;
    color: rgb(var(--clr));
    font-weight: 700;
    letter-spacing: -0.02em;
  }

  .legal-header p {
    margin: 0;
    color: rgb(var(--clr) / 60%);
    font-size: 14px;
  }

  .sections {
    display: flex;
    flex-direction: column;
    gap: 32px;
  }

  .legal-section {
    border-left: 3px solid rgb(var(--clrPrm));
    padding-left: 24px;
  }

  .legal-section h2 {
    margin: 0 0 12px 0;
    font-size: 18px;
    color: rgb(var(--clr));
    font-weight: 600;
  }

  .legal-section p {
    margin: 0 0 12px 0;
    color: rgb(var(--clr) / 80%);
    font-size: 15px;
    line-height: 1.6;
  }

  .legal-section p:last-child {
    margin-bottom: 0;
  }

  .legal-section ul {
    margin: 0 0 12px 0;
    padding-left: 20px;
    color: rgb(var(--clr) / 80%);
    font-size: 15px;
    line-height: 1.6;
  }

  .legal-section li {
    margin-bottom: 6px;
  }

  /* Mobile responsive styles */
  @media (max-width: 768px) {
    .legal-container {
      padding: 16px;
      align-items: flex-start;
    }

    .legal-content {
      padding: 24px 20px;
      max-width: 100%;
      border-radius: 12px;
    }

    .legal-header {
      margin-bottom: 32px;
      text-align: left;
    }

    .legal-header h1 {
      font-size: 26px;
    }

    .legal-section h2 {
      font-size: 16px;
    }

    .legal-section p,
    .legal-section ul {
      font-size: 14px;
    }

    .back-link {
      padding: 8px 12px;
      font-size: 13px;
    }
  }

  @media (max-width: 480px) {
    .legal-container {
      padding: 12px;
    }

    .legal-content {
      padding: 24px 16px;
      border-radius: 10px;
    }

    .legal-header h1 {
      font-size: 24px;
    }

    .legal-header p {
      font-size: 13px;
    }

    .legal-section {
      margin-bottom: 24px;
      padding-left: 16px;
    }

    .legal-section h2 {
      font-size: 15px;
      margin-bottom: 10px;
    }

    .legal-section p,
    .legal-section ul {
      font-size: 14px;
    }
  }
</style>
