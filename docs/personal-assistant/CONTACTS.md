# Contacts

Store and search your personal contacts -- names, phone numbers, emails, and relationships.

## Quick Example

```
You:    Add Sarah's number: 555-1234. She's my sister.
Butler: Contact added: Sarah (id: 1)

You:    What's Sarah's number?
Butler: Sarah -- phone: 555-1234, relationship: sister

You:    Find anyone named Ali
Butler: 1. Alice -- alice@example.com (friend)
```

## Tools

### `contact.add` -- Add a new contact

| Parameter | Type | Required | Notes |
|-----------|------|----------|-------|
| `name` | string | yes | Full name |
| `email` | string | no | Email address |
| `phone` | string | no | Phone number |
| `relationship` | string | no | e.g., "friend", "sister", "coworker" |

Capability: `data.contacts.write`

### `contact.search` -- Search contacts by name

| Parameter | Type | Required | Notes |
|-----------|------|----------|-------|
| `query` | string | yes | Partial name match (uses SQL `LIKE %query%`) |

Returns up to 20 matching contacts with id, name, email, phone, and relationship. Capability: `data.contacts.read`

## Table: `user_contacts`

```sql
id                INTEGER PRIMARY KEY
name              TEXT NOT NULL
phone             TEXT
email             TEXT
channel_ids       TEXT         -- JSON map of channel-specific identifiers
preferred_channel TEXT         -- which channel to reach them on
relationship      TEXT
notes             TEXT
birthday          TEXT
created_at        TEXT NOT NULL DEFAULT (datetime('now'))
```

Indexed on `name` for fast lookups.

## Planned Tools

- `contact.update` -- Edit existing contact fields
- `contact.birthdays` -- List upcoming birthdays

## Privacy

All contacts are stored locally in SQLite. Encrypted at rest when a database passphrase is configured. No cloud sync. Contact data never leaves your device.
