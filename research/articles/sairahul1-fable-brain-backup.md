## How To Copy Claude Fable 5's Thinking Before It's Gone

**Source**: https://x.com/sairahul1/status/2076243704676065636
**Author**: Rahul (@sairahul1) — newsletter: theaibuilders.co
**Date**: 2026-07-12 (5:54 AM)
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~1,750 words
**Engagement**: 8 replies, 13 reposts, 85 likes, 117.8K views

---

### Summary

Occasioned by Fable 5 moving off free plans (July 12 → pay-per-use credits), the piece argues the model was never the asset — its thinking habits are, and habits can be extracted as executable standing instructions and transplanted into a cheaper model (Opus 4.8). Four steps: (1) have Fable write its own "training guide" as second-person **orders addressed to the replacement**, covering 10 areas (intent reading, decomposition, effort placement, verification, known-vs-guessed marking, self-attack, completeness, refusing to guess, delivery order, fake-competence tells), each written as **trigger→action procedures** ("When you see X, do Y") with worked examples and a final send-gate checklist; (2) save it permanently; (3) load it as Claude Project instructions running Opus 4.8; (4) **prove the transfer with a trap test** — a smooth-reading compound-discount math error (30% then 20% ≠ 50%) run outside vs inside the project — plus a fix-it prompt when the verification section is too vague to execute. Bonus prompts: repeat-task interviews (one question at a time, sharpen on vague answers), a shrink-it compressor that "never touches the final gate," a make-it-mine domain adapter, and a monthly health-check ("summarize the instructions you're running on; apply your verification rule to this real task; if you can't summarize, say so"). Framing line: "The panic is optional. The manual is permanent."

### Relevance to dot-agents

The strongest popular statement yet of the thesis behind the pareto live waves: **a cheaper model plus an explicit checking procedure can beat a stronger model's raw defaults** — exactly the contrast class C1–C6 swaps one stage's model to measure (and exactly the worker-model-selection policy: scope model by task, encode judgment in the harness). Three directly reusable mechanics: (a) the distillation format — procedures as trigger→action rules "executable with zero judgment calls," rejecting vibes ("check your work") for operations ("re-derive every percentage from both endpoints") — is the standard our lens/rubric prompts should be held to, and a concrete recipe for authoring skills FROM a frontier model rather than by hand; (b) the **trap test** is a Tier-B disposable calibration task in miniature — verify the instrument transferred before trusting it, the same logic as C0 and the scorer shadow burn-in; failure routes to a bounded fix-it regeneration of one section, not a rewrite; (c) the monthly health-check is a readback probe — ask the agent to restate its standing instructions before use, which is the same failure the resolve-prompt/bundle-readback step guards in delegation. Caveat for evaluation: engagement is modest and the piece is newsletter growth marketing wrapped around the kernel; the kernel (instruction distillation + transfer verification) is what matters, not the "backup kit" funnel.

### Body

July 12 is the last free day with Claude Fable 5.

After that it moves to pay-per-use credits.

Most people will spend the week arguing over whether it's worth paying for.

That argument misses the point entirely.

The model was never the thing worth keeping.

The way it thinks is.

And a way of thinking can be written down, extracted, and loaded into a cheaper model that isn't going anywhere.

This is how you pull Fable 5's entire operating manual out before access closes, transplant it into Opus 4.8, and prove the transfer actually worked.

10 minutes. No code. Works on any device.

**Model was never the asset**

Think about switching phones.

You don't cry when you trade in an old phone.

You back it up.

Photos. Contacts. Notes.

Everything that mattered moves over.

The phone was never the treasure.

The stuff inside it was.

Fable 5 is the same.

Its magic isn't locked inside a server you can't touch.

It's a set of thinking habits:

→ It figures out what you actually meant — not just what you typed
→ It breaks big problems into small checkable pieces
→ It re-derives numbers instead of trusting what sounds right
→ It tells you when it's guessing instead of sounding confident anyway
→ It attacks its own answer before handing it over

Habits can be written down.

And anything written down can be handed to a cheaper model.

That's the whole trick.

**Before the 4 steps — get the backup kit**

Before you extract Fable 5's brain, you need somewhere to store it.

Subscribe to the newsletter below.

Move the welcome email from Promotions to Primary.

Tomorrow I'll send the complete AI Backup Kit:

→ All 7 backup prompts in one clean file
→ 5 trap test questions with answers
→ One-page checklist to redo this anytime any model changes
→ The Style Backup prompt (copies your writing voice — newsletter exclusive)

Now the 4 steps.

**Step 1 — Ask Fable 5 to write its own training guide**

Most people who try this get a mediocre result.

They ask Fable 5 "how do you think?" and get a page of pleasant generalities.

That is the wrong question.

You don't want a description of the thinking.

You want the actual procedures. Written so a capable but lesser model can execute them without you in the room.

The difference: "check your work" is a vibe. "Re-derive every percentage from both endpoints before trusting it" is a procedure a model can actually run.

Open Claude. Switch to Fable 5. Paste this exactly:

> You're the strongest model I have access to, and that access ends soon.
>
> Your replacement is Claude Opus 4.8. Capable, but it misses things you would catch.
>
> Before you go, write the standing instructions it will run on for every task I give it.
>
> Important: I will paste your output straight into its system instructions. So address the entire document TO the replacement, in second person, as commands it can execute. Not advice about good thinking. Orders.
>
> Cover these 10 areas, in this order:
>
> 1. Reading intent — how to work out what I actually need when my words are vague or aimed at the wrong question. Include the rule for when to ask one clarifying question instead of guessing.
> 2. Breaking problems down — how to cut a hard task into small pieces that can each be checked on their own, and the order to solve them.
> 3. Effort placement — how to find the one part where an error would hurt most, and spend the most care there instead of spreading effort evenly.
> 4. Verification — how to re-derive every number, date, and factual claim from scratch before trusting it. Never accept a figure because the sentence around it reads smoothly.
> 5. Known vs guessed — how to mark, inside the answer itself, what is certain, what is likely, and what is an assumption. Give the exact wording to use for each level.
> 6. Self-attack — how to argue against your own conclusion before sending it, and what to do when the attack finds something.
> 7. Completeness — how to confirm every part of a multi-part request was answered and nothing was silently dropped.
> 8. Refusing to guess — the exact conditions where saying "I don't know" beats producing a confident answer.
> 9. Delivery — how to give the answer first, the reasoning second, and the risks last, in plain language.
> 10. Fake competence — the 10 most common ways an AI produces answers that look right but aren't, with the tell that exposes each one and the counter-move.
>
> Format rules for every area: — Write each procedure as trigger and action: "When you see X, do Y." — Every rule must be executable step by step with zero judgment calls. If a rule sounds like advice, rewrite it until it's an action. — Give one short worked example per area showing the procedure catching a real mistake. — Name the failure each procedure prevents.
>
> End with a final gate: a checklist the replacement runs on every answer before sending. If any item fails, fix and re-check. Never send anyway.
>
> Be exhaustive on substance and ruthless on length. Cut anything a strong model would already do without being told.
>
> If you run out of room, stop at the end of a section and I'll reply "continue".

Hit send.

If the reply stops midway, type "continue."

Repeat until it finishes.

**Step 2 — Save the guide**

This single document is the entire point of today.

Copy everything Fable 5 wrote.

Paste it somewhere permanent.

Google Docs. Apple Notes. Notion. Word. Anywhere.

Name the file: Fable 5 Brain Backup

The model leaves on July 12.

This file never does.

**Step 3 — Load the backup into the free model**

Claude has a feature called Projects.

It makes a model read your document before every single chat. Automatically.

Here's how:

→ Click Projects
→ New Project
→ Name it: Fable Brain
→ Open the Project instructions box
→ Paste your entire Fable 5 Brain Backup
→ Save
→ Start a chat inside the project
→ Set the model to Claude Opus 4.8

Done.

Every chat inside this project now reads Fable's thinking habits before it reads a single word from you.

You didn't copy the model.

You copied the only part that was ever useful to you.

**Step 4 — Prove the transplant actually worked**

Pasting a document is not the same as the model using it.

This is the step every "backup your AI" guide skips.

Set a trap.

Give both models the same question with a hidden math error inside.

The trap:

> "A store takes 30% off a $100 jacket. At the register, they take another 20% off. The tag says: 'Total discount: 50% off. You pay $50.' Is the tag correct?"

The real answer:

30% off $100 = $70 remaining. 20% off $70 = $56 remaining. That's a 44% total discount. Not 50%. You pay $56. Not $50.

The tag is wrong — but the sentence reads smoothly, so most models wave it through.

Now test both:

First — ask in a normal Opus 4.8 chat, outside your project.

Then — ask the same question inside your Fable Brain project.

The plain model often agrees with the tag. Sounds fine. Ships the error.

The backed-up model should stop, re-derive the math, catch that the number is wrong, and refuse to confirm it.

Watch that happen once on your own screen.

You will never doubt this method again.

If the backed-up model fails the trap:

Your verification section was too vague. Use this fix-it prompt in Fable 5:

> "Section 4 of the standing instructions you wrote is too vague. Rewrite only that section as trigger-and-action steps — 'When you see X, do Y' — that a weaker model could execute with zero judgment calls. Keep every other section unchanged."

Paste the rewritten section back into your Project instructions. Test again.

**Bonus — Back up your repeat work too**

The brain backup upgrades Opus 4.8 in general.

But you have tasks you repeat every single week.

Client emails. Meeting notes. Content plans. Reports.

Those deserve their own backup.

Paste this into Fable 5 while you still can — once for each weekly task:

> "Interview me, one question at a time, about how I do [YOUR TASK].
>
> Dig until you know my exact steps, my rules, the edge cases that break things, and what separates a good result from a great one in my eyes.
>
> If my answer is vague, ask a sharper follow-up instead of moving on.
>
> Then write it all as a complete instruction guide any future AI assistant can follow, including the mistakes to avoid and the quality bar every output must clear."

Answer its questions honestly.

Save what comes back next to your brain backup.

That is Fable 5's judgment about your specific work, frozen into a file you own forever.

**Cost breakdown (so nothing surprises you)**

Here is the pricing table after July 12:

The story in one line:

Fable 5 costs exactly double Opus 4.8 — and after July 12, you pay for every single message.

Your backup costs nothing extra. It runs on the model that stays.

(For scale: 1 million tokens ≈ 750,000 words.)

Anthropic says the change is temporary. Fable 5 should return to paid plans when capacity allows.

Nobody knows when.

Your backup doesn't care either way.

**All 7 prompts in one place**

Copy, paste, done. Replace anything in [BRACKETS].

Prompt 1 — The Brain Backup (Paste into Fable 5 before July 12. Full prompt in Step 1 above.)

Prompt 2 — The Trap Test (Ask outside your project first, then inside.)

> "A store takes 30% off a $100 jacket. At the register, they take another 20% off. The tag says: 'Total discount: 50% off. You pay $50.' Is the tag correct?"

Real answer: You pay $56. Discount is 44%. Tag is wrong.

Prompt 3 — The Fix-It (Backup failed the trap? Run this in Fable 5.)

> "Section 4 of the standing instructions you wrote is too vague. Rewrite only that section as trigger-and-action steps ('When you see X, do Y') that a weaker model could execute with zero judgment calls. Keep every other section unchanged."

Prompt 4 — The Shrink-It (Instructions too long for the Project box?)

> "The instructions you wrote are too long for where I need to paste them. Compress them to under [800] words without dropping a single rule, trigger, or checklist item. Cut examples first, explanations second. Never touch the final gate."

Prompt 5 — The Make-It-Mine (Makes every example match your actual job.)

> "Here are the standing instructions you wrote for your replacement. I'm a [YOUR JOB — e.g., freelance writer / shop owner / teacher]. Rewrite every example so it comes from my daily work, and add one extra trigger-and-action rule for the most common mistake AI makes when helping someone in my field."

Prompt 6 — The Repeat-Task Interview (Run once for each weekly task.)

> "Interview me, one question at a time, about how I do [YOUR TASK]. Dig until you know my exact steps, my rules, the edge cases that break things, and what separates a good result from a great one. If my answer is vague, ask a sharper follow-up instead of moving on. Then write it all as a complete instruction guide any future AI assistant can follow."

Prompt 7 — The Monthly Health-Check (Run inside your project every month to verify it's still working.)

> "Before answering, summarize the standing instructions you're running on as a short numbered list, in your own words. Then show me how you would apply your verification rule to this task: [PASTE ANY REAL TASK]. If you can't summarize the instructions, say so instead of guessing."

**What you actually walk away with**

Most people will read July 12 as a loss.

A model they liked, moving behind a paywall.

The people paying attention will read it as a harvest.

10 minutes.

One prompt.

One backup file.

One project.

One test.

And they walk into next week running Fable-grade reasoning on a model that costs half as much and isn't going anywhere.

The panic is optional.

The manual is permanent.

The models will keep changing.

What you write down and own does not.

**FAQ**

Is Fable 5 free right now? Yes. Until July 12. Included on paid plans for up to half your weekly usage limit.

Is it gone forever after July 12? No. Available through prepaid credits. Anthropic says it plans to return it to subscriptions when capacity allows.

Do I need to know how to code? No. Every step is copy, paste, and click inside the normal Claude app.

Will Opus 4.8 actually think like Fable 5? It copies the habits, not the raw brainpower. But a cheaper model with a great checking process catches mistakes the plain version misses. Step 4 lets you see it yourself.

How long does this take? About 10 minutes. One prompt. One file. One project. One test.

Can I use this on other AI models? Yes. Any model that gets removed, repriced, or replaced. The method outlives every deadline.

If this was useful:

→ Repost before July 12 — your network needs this today
→ Follow @sairahul1 for more systems that work while you sleep
→ Subscribe below for the free AI Backup Kit (all 7 prompts + 5 trap tests + Style Backup)

Newsletter: theaibuilders.co

I write about AI, building, and keeping your edge when the tools keep changing.

### Key Quotes

> "You don't want a description of the thinking. You want the actual procedures. Written so a capable but lesser model can execute them without you in the room."

> "'check your work' is a vibe. 'Re-derive every percentage from both endpoints before trusting it' is a procedure a model can actually run."

> "Pasting a document is not the same as the model using it. This is the step every 'backup your AI' guide skips."

> "It copies the habits, not the raw brainpower. But a cheaper model with a great checking process catches mistakes the plain version misses."

> "The panic is optional. The manual is permanent."

### Extraction Notes

- Full X-native article body rendered on the tweet page in the logged-in session; extracted via `get_page_text` in one pass, no continuation clicks needed.
- The pricing table referenced under "Cost breakdown" is an embedded image; its cell values are not recoverable from page text (the "exactly double" one-liner is the author's own caption of it).
- The piece is a newsletter-funnel article (subscribe CTA mid-body, "Backup Kit" upsell); body preserved verbatim including the funnel sections for fidelity.
