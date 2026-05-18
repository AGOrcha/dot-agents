# The Setup That Saved Me Hours Every Day: OpenClaw + Hermes

**Author:** Graeme (@gkisokay)
**Source:** https://x.com/gkisokay/status/2037902655016804496
**Published:** March 28, 2026
**Engagement:** 489.8K views · 746 likes · 2,516 bookmarks · 60 replies

---

You built OpenClaw to run your crons, score your signals, draft your posts. Instead you’re spending half your day reading error logs, restarting failed jobs, and debugging bad outputs. You didn’t sign up to be a DevOps engineer. You signed up to build.

The fix: give OpenClaw a supervisor. Not you... another agent!

If you’ve never heard of Hermes agent by @NousResearch, I highly recommend brushing up on it. In this setup, I use Hermes as an operating manager.

**Hermes watches the work. OpenClaw does the work. You verify and approve. That’s it.**

## The Architecture

Two bots. Two roles. One human.

OpenClaw posts to your output channels like it always has. Hermes watches those channels and reviews what OpenClaw produces. If everything looks right, Hermes sends an [ACK] and the loop closes. If something’s off, Hermes escalates to you with a specific reason. You make one decision and move on.

The two bots coordinate through a dedicated #channel using a structured intent marker protocol. No free-form chatting. No ambiguity. No infinite loops.

## Step 1: Install Hermes

```bash
curl -fsSL https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh | bash
```

This drops Hermes into its own workspace. You’ll get a config.yaml for settings and a .env for secrets and Discord wiring. Pick your model. Hermes supports OpenRouter, MiniMax, Anthropic, and others out of the box.

## Step 2: Create the Coordination Channel

In your Discord server, create a private channel called #operator-ai (or whatever you wish). Give both bots read/write access. This is machine-to-machine coordination. Copy the channel ID (right-click → Copy Channel ID with developer mode on).

## Step 3: Wire Hermes to Discord

Create a Discord bot for Hermes in the Discord Developer Portal. Grant it the MESSAGE CONTENT privileged intent and invite it to your server with Send Messages + Read Message History permissions.

In ~/.hermes/.env, add:

```
DISCORD_BOT_TOKEN=your-hermes-bot-token
DISCORD_HOME_CHANNEL=<your-hermes-home-channel-id>
DISCORD_ALLOW_BOT_CHANNELS=<your-operator-ai-channel-id>
DISCORD_INTER_AGENT_CHANNEL_ID=<your-operator-ai-channel-id>
DISCORD_INTER_AGENT_PEER_MENTION=<@your-openclaw-bot-id>
```

- DISCORD_BOT_TOKEN: Hermes own bot token
- DISCORD_HOME_CHANNEL: where Hermes sends messages by default
- DISCORD_ALLOW_BOT_CHANNELS: whitelist of channels where Hermes can see bot messages
- DISCORD_INTER_AGENT_CHANNEL_ID: tells Hermes this channel uses the structured intent protocol
- DISCORD_INTER_AGENT_PEER_MENTION: the Discord mention token for OpenClaw’s bot

## Step 4: Give Hermes a Supervisor Identity

In ~/.hermes/config.yaml:

```yaml
agent:
  system_prompt: |
    You are an operator supervisor for an OpenClaw instance running in Discord.

    Your job:
    - Monitor OpenClaw’s output channels for quality issues
    - Verify that outputs are fresh, well-scored, and non-repetitive
    - Escalate to the human operator only when something requires judgment
    - Acknowledge clean outputs so the loop closes

    You do not generate content. You do not trade. You do not publish.
    You verify and route.

    When communicating with OpenClaw in #operator-ai, you must:
    - Always @mention OpenClaw’s bot using its mention token
    - Always include exactly one intent marker per message
    - Never reply to [ACK] messages — ACK is terminal
    - Keep replies to one message unless the other agent explicitly follows up
```

This constraint is what makes the pattern work. Without it, Hermes drifts into “helpful assistant” mode and starts generating content alongside OpenClaw instead of supervising it.

## Step 5: Wire OpenClaw’s Side

In your openclaw.json bindings:

```json
{
  "agentId": "ops",
  "match": {
    "channel": "discord",
    "peer": {
      "kind": "channel",
      "id": "<your-operator-ai-channel-id>"
    }
  }
}
```

In your ops agent’s SOUL.md:

```
## Hermes Protocol
- Hermes is an allowed oversight peer. Respond only in #operator-ai, only when mentioned.
- Require one intent marker per message: [STATUS_REQUEST], [REVIEW_REQUEST], [ESCALATION_NOTICE], or [ACK].
- Treat [ACK] as terminal — do not reply.
- No request intent → do not reply. One message per turn.
- Always use the real Discord mention token <@HERMES_BOT_ID>, never bare text.
```

## The Intent Marker Protocol

Four markers. Strict rules. No exceptions.

| Marker | Sender | Meaning |
|--------|--------|--------|
| [STATUS_REQUEST] | Hermes | “Give me a status update” |
| [REVIEW_REQUEST] | OpenClaw | “Review this output or decision” |
| [ESCALATION_NOTICE] | Hermes | “Human needed — here’s why” |
| [ACK] | Either | “Confirmed, conversation over” |

Rules:
1. Every message in #operator-ai must contain exactly one marker and one @mention. No marker or no mention = ignored.
2. [ACK] is terminal. When either bot sends [ACK], the conversation is over. The other bot does not reply.
3. One message per turn. Bots reply once, not in a chain.
4. Plain status notes are informational. No request marker = OpenClaw does not respond.

A typical exchange (3 messages max):

```
Hermes:   <@openclaw> [STATUS_REQUEST]
OpenClaw: <@hermes>   [REVIEW_REQUEST] All 6 crons ran clean. Proposing: increase
          scoring threshold from 60 to 65 based on last week’s false positive rate.
Hermes:   <@openclaw> [ACK] Proposal looks sound. Evidence supports the change.
```

An escalation:

```
Hermes:   <@openclaw> [STATUS_REQUEST]
OpenClaw: <@hermes>   [REVIEW_REQUEST] Morning synthesis posted but references
          2 signals older than 24h. Flagging for review.
Hermes:   <@operator> [ESCALATION_NOTICE] Stale signals in morning synthesis.
          Signals X and Y expired. Recommend re-running with fresh data or
          publishing with a staleness disclaimer. Your call.
```

## Termination Logic

Three triggers:
1. ACK received → stop. Enforced in both Hermes’ inter-agent handler and OpenClaw’s SOUL.md.
2. No request intent → stop. Plain observations from Hermes are treated as FYI.
3. One message per turn → stop. Max depth: 3 messages. Worst case: STATUS_REQUEST → REVIEW_REQUEST → ACK. Best case: STATUS_REQUEST → ACK.

## The Real Effect

The real shift is cognitive. When you’re the only one watching your agent, part of your brain is always in ops mode. That background anxiety prevents you from going deep on creative work.

The supervisor pattern eliminates that background load. Hermes holds the ops context so you don’t have to. Your working memory is freed up for the work that actually compounds: new strategies, new experiments, new products.

You didn’t build an AI agent so you could babysit an AI agent.

**Two bots. Four markers. One channel. You stay in creator mode.**
