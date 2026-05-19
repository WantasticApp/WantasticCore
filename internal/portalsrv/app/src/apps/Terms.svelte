<script lang="ts">
</script>

<div class="legal-container">
  <div class="legal-content">
    <div class="legal-header">
      <a href="/" class="back-link">← Back Home</a>
      <h1>Terms of Service</h1>
      <p>Last updated: January 2026</p>
    </div>

    <div class="sections">
      <section class="legal-section">
        <h2>1. Agreement to Terms</h2>
        <p>
          By accessing and using Wantastic, you agree to be bound by these Terms
          of Service. If you disagree with any part of these terms, you may not
          use our service.
        </p>
      </section>

      <section class="legal-section">
        <h2>2. Use License</h2>
        <p>
          Permission is granted to temporarily download one copy of the
          materials (information or software) from Wantastic for personal,
          non-commercial transitory viewing only. This is the grant of a
          license, not a transfer of title, and under this license you may not:
        </p>
        <ul>
          <li>Modify or copy the materials</li>
          <li>
            Use the materials for any commercial purpose or for any public
            display
          </li>
          <li>Attempt to decompile or reverse engineer any software</li>
          <li>
            Remove any copyright or other proprietary notations from the
            materials
          </li>
          <li>
            Transfer the materials to another person or "mirror" the materials
            on any other server
          </li>
          <li>Use the service to conduct illegal activities</li>
        </ul>
      </section>

      <section class="legal-section">
        <h2>3. Service Description</h2>
        <p>
          Wantastic provides cloud-based MikroTik device management including
          WireGuard VPN services, WebSSH terminals, and Winbox proxy access.
          Account limits (such as the number of WireGuard peers) are configured
          per tenant by the platform administrator.
        </p>
      </section>

      <section class="legal-section">
        <h2>4. Acceptable Use Policy</h2>
        <p>You agree not to use our service for:</p>
        <ul>
          <li>Unauthorized access to computer systems</li>
          <li>Transmitting malware or harmful code</li>
          <li>Phishing or social engineering attacks</li>
          <li>DDoS attacks or network abuse</li>
          <li>Violation of applicable laws and regulations</li>
          <li>Harassment, hate speech, or threats</li>
          <li>Copyright infringement or IP violations</li>
          <li>Any activity that interferes with service availability</li>
        </ul>
      </section>

      <section class="legal-section">
        <h2>5. Account Responsibility</h2>
        <p>
          You are responsible for maintaining the confidentiality of your login
          credentials and are fully responsible for all activities that occur
          under your account. You agree to immediately notify us of any
          unauthorized use of your account.
        </p>
      </section>

      <section class="legal-section">
        <h2>6. Limitation of Liability</h2>
        <p>
          Wantastic and its suppliers will not be liable for any damages
          (including, without limitation, damages for loss of data or profit, or
          due to business interruption) arising out of the use or inability to
          use the materials on Wantastic, even if we or our authorized
          representative has been notified orally or in writing of the
          possibility of such damage.
        </p>
      </section>

      <section class="legal-section">
        <h2>7. Disclaimer of Warranties</h2>
        <p>
          The materials on Wantastic are provided on an 'as is' basis. We make
          no warranties, expressed or implied, and hereby disclaim and negate
          all other warranties including, without limitation, implied warranties
          or conditions of merchantability, fitness for a particular purpose, or
          non-infringement of intellectual property or other violation of
          rights.
        </p>
      </section>

      <section class="legal-section">
        <h2>8. Service Level Agreement</h2>
        <p>
          We aim for 99.5% uptime. Maintenance windows and force majeure events
          are excluded. Contact support@wantastic.app to report SLA breaches.
        </p>
      </section>

      <section class="legal-section">
        <h2>9. Modifications to Terms</h2>
        <p>
          Wantastic may revise these terms of service for its website at any
          time without notice. By using this website you are agreeing to be
          bound by the then current version of these terms of service. We will
          notify users of material changes via email.
        </p>
      </section>

      <section class="legal-section">
        <h2>10. Governing Law</h2>
        <p>
          These terms and conditions are governed by and construed in accordance
          with the laws of the jurisdiction where Wantastic operates, and you
          irrevocably submit to the exclusive jurisdiction of the courts in that
          location.
        </p>
      </section>

      <section class="legal-section">
        <h2>11. Termination</h2>
        <p>
          We may terminate or suspend your account and access to our service
          immediately, without prior notice or liability, for any reason
          whatsoever, including if you breach these Terms. Upon termination,
          your right to use the service will immediately cease.
        </p>
      </section>

      <section class="legal-section">
        <h2>12. Contact Information</h2>
        <p>
          If you have any questions about these Terms of Service, please contact
          us at:
        </p>
        <p>Email: legal@wantastic.app</p>
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
