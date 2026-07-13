## How I Run an AI Agent Without Touching My Phone

**Source**: https://x.com/madebydia/status/2069413760540852403
**Canonical body source**: https://blog.raisingpixels.dev/p/how-i-run-an-ai-agent-without-touching (author Substack cross-post of the X-native article; author site https://raisingpixels.dev, product https://buildwithyourkid.com)
**Author**: Diana Park (@madebydia)
**Date**: 2026-05-30 (Substack post; tweet shared 2026-06-23)
**Method**: Playwright (x.com tweet — verified link card) + WebFetch (author Substack for verbatim body)
**Word count**: ~1,100 words

---

### Summary

Reframes AI-agent UX around presence/attention rather than throughput: the interface, not the agent, is the problem, because the input device sends a social signal (especially to a watching child). Uses three capture tiers — fast (Apple Watch dictation routed to a Hermes agent via a Poke iMessage executor), slow (smart pen / notebook, classified into the pipeline later), and none (scheduled/cron automation). Automation's real value is removing the decision point before the task, not just saving task-minutes. (Tagged "related concepts to draw on later" in the brief.)

---

### Body

My 3-year-old doesn't know what AI is. He knows that mom sometimes talks to her watch, scribbles in a notebook, and that things happen because of it — a game appears on his computer, groceries show up, a recipe prints itself. He thinks this is normal. He's right.

The strange part is not that I use an AI agent. The strange part is that, for a while, the only way to talk to it looked exactly like not being present.

My phone is the command surface for half my life. It is also the most attention-capturing object in the house. Every time I picked it up to send a one-line instruction that would save me an hour later, my son saw the same thing every child sees: Mom chose the rectangle over him. The task got done. Something else got lost.

So I started rebuilding the interface. Not because I wanted less technology — I wanted less *visible extraction*. The problem was not the agent. The problem was the surface.

### What the agent does

I run **Hermes**, an AI agent framework that gives me a persistent assistant I can talk to over iMessage. It handles the boring connective tissue of family life: email summaries, meal planning, homeschool tracking, draft capture, reminders, research queues, small logistics. The default interface, though, was texting — picking up my phone — unlocking the black hole. One quick instruction became one notification, then one glance at email, then ten minutes of tiny unrelated decisions. Even when I resisted, the visual signal had already been sent. So I moved the commands somewhere else.

### Fast capture: the Watch

For quick, disposable commands, I use my Apple Watch. Raise wrist, dictate, lower wrist. On the other end, **Poke** gets the iMessage and executes — turning the Watch into a real command surface, not just a smaller keyboard. Perfect for "mental lint": *"Add printer paper to the shopping list." "Remind me after bedtime to order tracing paper." "Capture this: he said the red car game needs a mountain road."* The Watch is not universal — dictation mangles technical terms and it's terrible for review — but for the ~80% of commands that are load-bearing scraps, it's exactly right. My son sees a raised wrist and a sentence, then gets me back.

### Slow capture: the notebook

For anything that needs to think before becoming a command, I use paper — an **inq pen** that digitizes handwriting from a dot-pattern notebook. I write by hand, it transcribes, and I send the result into the Hermes pipeline later. This changes the *social meaning* of the act: a parent with a phone looks absent even when useful; a parent with a notebook looks like a parent with a notebook. The important part is not the transcription — it's that I didn't have to hold the thought in my head and didn't have to disappear into a screen to preserve it. A pocket notebook, index cards, or a kitchen whiteboard would do most of this; the principle is older than every device: write it down somewhere that does not open into the internet.

### No capture: scheduled work

The best interface is no interface at all. A lot of what the agent does shouldn't require a command at all. Hermes runs scheduled tasks: checking email at intervals, monitoring mentions, building overnight summaries, preparing reports while I sleep. Automation isn't just about saving the minutes spent doing the task — **it removes the decision point before the task.** Cognitive load stops existing. This is also where the system becomes least visible to my child.

### The rule underneath the gear

You don't need Hermes, an inq pen, or an Apple Watch. The principle is interface design, not gear:
- **Move recurring work off your hands.** Whatever you check daily should come to you, not the other way around (cron jobs, iOS Shortcuts, scheduled self-emails). Convert "I need to remember to check this" into "it'll show up when it matters."
- **Pick a not-phone surface for live capture.** The Watch and pen work because they don't look like scrolling. A notebook or fridge whiteboard does the same. The input device your child sees shouldn't be the same shape as Instagram.
- **Batch the phone into windows.** Morning, midday, after bedtime; otherwise it's in another room. This is the rule that makes the rest possible.
- **Design for what they see.** Your kid can't tell "mom is doing something important" from "mom is scrolling." They see the rectangle. Pick the interface that sends the message you want them to receive.

### What my son sees

Right now he sees a mom who writes in notebooks, talks to her watch sometimes, and gets things done without staring at a screen. For now, he sees the outcome: a parent whose attention returns quickly. That's the output I'm shipping.

(The post closes with a soft CTA to a 12-week screen-free "computational thinking" curriculum for ages 2–6 at buildwithyourkid.com.)

---

### Key Quotes

> "The strange part is not that I use an AI agent. The strange part is that, for a while, the only way to talk to it looked exactly like not being present."

> "Automation is not just about saving the minutes spent doing the task. It removes the decision point before the task."

> "The best interface is no interface at all."

> "Your kid can't tell the difference between 'mom is doing something important' and 'mom is scrolling.' They see the rectangle."

---

### Extraction Notes

The x.com tweet (`/status/2069413760540852403`) was navigated via Playwright (`article-extract` skill, Path B) and confirmed to be a bare link card to an X-native article (login-walled). The verbatim body above is from the author's own Substack cross-post of the identical piece, fetched via WebFetch.
