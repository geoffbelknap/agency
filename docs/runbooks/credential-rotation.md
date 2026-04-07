# Credential Rotation

## Trigger

Scheduled rotation, compromised API key, expired credential, or provider key change.

## Steps

### 1. Identify affected credential

```bash
agency creds list
agency creds show <credential-name>
```

### 2. Rotate the credential

```bash
agency creds rotate <credential-name> --value <new-value>
```

This updates the encrypted credential store and regenerates `credential-swaps.yaml` for the egress proxy.

### 3. Test the new credential

```bash
agency creds test <credential-name>
```

Expected: test passes with the new value.

### 4. Reload egress proxy

The egress proxy picks up credential changes via `credential-swaps.yaml` hot-reload. Verify:

```bash
agency infra status
```

If the egress proxy doesn't pick up the change:

```bash
agency infra rebuild egress
```

### 5. Verify agents can use the new credential

```bash
agency send <agent-name> "Test the <service> integration."
```

## Compromised Key — Emergency Procedure

If a key is known or suspected compromised:

### 1. Revoke at the provider immediately

Go to the provider's dashboard and revoke/regenerate the key. Don't wait for Agency rotation.

### 2. Rotate in Agency

```bash
agency creds rotate <credential-name> --value <new-value-from-provider>
```

### 3. Check audit trail

```bash
agency log <agent-name>    # for each agent that used this credential
agency admin audit
```

Look for unusual API calls or data access during the compromise window.

### 4. Restart affected agents

```bash
agency stop <agent-name>
agency start <agent-name>
```

This ensures the enforcer and egress proxy are using the new credential.

## Credential Groups

For services using JWT exchange or shared auth config (e.g., LimaCharlie):

```bash
# List credentials filtered by group
agency creds list --group <group-name>

# Rotate a credential within a group
agency creds rotate <key-name> --value <new-value>
```

The group configuration (`agency creds group create`) defines the protocol — individual key rotation within a group works the same as standalone rotation.

## Verification

- [ ] `agency creds test <credential-name>` passes
- [ ] `agency infra status` shows egress healthy
- [ ] Agent successfully uses the rotated credential
- [ ] Old credential value no longer works (verify at provider)
