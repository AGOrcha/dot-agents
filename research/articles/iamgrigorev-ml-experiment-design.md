## How to design good ML experiments and actually learn from them

**Source**: https://x.com/iamgrigorev/status/2071688181628678468
**Author**: George Grigorev (@iamgrigorev), Poolside
**Date**: 2026-06-29 (4:12 PM)
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~2,800 words
**Engagement**: 11 replies, 22 reposts, 412 likes, 816 bookmarks, 54.3K views

---

### Summary

Runs ~100 experiments/week on Poolside's architecture team. Argues high experiment throughput only compounds with disciplined design: every experiment starts from an explicit research question, runs against a dated shared baseline config, finishes (no impatient early kills), and gets logged in a manual table. Evaluation goes beyond same-size baseline deltas: occasional scaling-law fits convert loss differences into a compute-efficiency-gain ratio (C_predicted / C_observed), which is the interpretable go/no-go metric. Research taste = preferring cheap, interpretable, composable ideas ("go wide first"); failed ideas point to better successors via citation-chasing (layernorm scaling → ProRes).

### Relevance to dot-agents

Reinforces the prototype-experiment-fidelity-gate lesson and the r4 eval harness discipline: faithful inputs, negative controls, no hidden losses. The dated-baseline-config process is the direct analogue of pinned eval baselines for the transcript-analysis pipeline; "an unfinished experiment is just compute spent" and the manual run-table map onto the iter-log / evidence-persistence discipline; "the evaluation should match the question" is the same rule as matching verifier lenses to the task.

### Body

Currently I launch around 100 experiments a week at Poolside. In our architecture team, I am probably the person with the most runs and ideas happening all the time. No night or weekend is spent without some new experiment done. This is good, because the current setup makes it possible to go much wider than before. You can have 5–10 agent sessions running in parallel. You most certainly have an advanced X feed with recent research ideas and papers. You can find papers yourself or pick up ideas from what other people are trying.

A good experiment starts with a research question. It is launched against a proper baseline. It is reproducible. It finishes. It is logged in a way that you can actually remember later. And if the result looks good, eventually you need to understand whether it scales.

**Start with the research question**

Before implementing anything, you should know what you actually want to test. Otherwise you can easily spend a day, or a week, launching something that will not give you a useful answer.

Sometimes the question is how this change contributes to quality, but sometimes you might have more foundational questions. For example, you might ask which aspect ratio is the most optimal, whether you can optimize training speed, whether reducing the sliding window size or the amount of global attention layers is viable, whether some part of the architecture can be simplified while keeping the same quality, or whether a change helps because it improves optimization, reduces communication, or removes useless computation.

The important part is that the experiment should have a reason to exist. If you do not have a concrete question, chances are the experiment will either not work, or the result will be hard to interpret, or the approach will not be beneficial even if the number looks fine. This is a very common failure mode. If the experiment did not start from a question, the answer is usually ambiguous. So before launching, I like to make the question explicit, at least in my own mind and in the experiment notes. The experiment should be able to answer something. Otherwise it is just a run.

**Where ideas come from**

There are many sources of ideas. They can come from recent papers, or from older papers and the references they cite. They can come from X, from resources such as NanoGPT Speedrun, or from internal discussions within your team. Ideas also often emerge when you analyze bottlenecks in your own training framework. And sometimes, they simply come from experience—after spending enough time looking at configs, training curves, and benchmark tables, patterns start to form and new ideas naturally follow.

There are always more ideas than time. The important skill is deciding which ideas are worth testing now, which ones should be saved for later, and which ones are probably too expensive for the amount of signal they can give. This is why research taste matters so much.

**Develop research taste**

By research taste, I mean the ability of finding ideas that align with your skill, ideas that are fruitful, easy enough to test, and likely to teach you something. Most of the time, it is better to focus on simple ideas first. Simple ideas are easier to implement, easier to debug, easier to compare, and easier to combine with other ideas.

This is especially important in architecture research. A simple idea developed in a simple training framework might not look very impressive. But the same idea developed in a more complicated and optimized training framework can shine. That is our case at Poolside. Our training framework is really good. It is optimized around communication-computation overlap and parallelism settings, and our transformer implementation is amazing. Because of that, it is a pleasure to add simple features to it and keep them on a limited basis. A small change can be tested cleanly. If it works, it can be combined with other changes. If it does not work, it still teaches something about the bottleneck.

This is why I usually prefer going wide first, not deep first. Going wide means testing many simple ideas, understanding the surface area, and seeing where there is signal. Going deep means spending much more time on one large direction. You eventually need both, and as a team you probably need to keep a few high-caliber ideas up your sleeve in order to win. But going deep too early is often a mistake.

For example, most of the time I would not start by changing attention to a very complicated sparse attention variant. I would not immediately implement Gated DeltaNet from scratch, HYSparse from MIMO, or a random SSM that requires custom kernels in Cute-DSL. These ideas might be important. Some of them might even be the right long-term direction. But for a normal experimentation setting, they are often too expensive unless you are very confident they will work.

For most experiments, the best ideas are not the most impressive-looking ideas. They are the ideas that are cheap enough to test, clear enough to interpret, and likely enough to combine with other improvements.

**Simple ideas can become strong ideas**

Simple ideas are valuable because they can compound. An idea can be small on its own, but useful when combined with other changes.

One example for me is layernorm scaling. I always liked this approach from The Curse of Depth paper. Conceptually, it felt like a solid way to think about deeper transformers. The intuition made sense to me. In reality, it does not scale well enough.

But the direction was still interesting. So instead of dropping it completely, I looked through the references and newer papers citing the original norm scaling idea. That is how I found ProRes, Progressive Residual Warmup. The idea is a better version of a similar intuition. Each layer's residual connection is multiplied by a scalar value that gradually increases, or "warms up," from 0 to 1 over the course of training. The warm-up depends on the layer id, so earlier layers warm up faster and deeper layers warm up slower. This actually works. It is nice.

The important part is not only the specific method. The important part is that a failed idea can point to a better one. If you understand why you liked the idea originally, you can search for newer versions, related work, citations, or modifications that preserve the intuition but fix the failure mode. Research is not just isolated experiments. It is also a long-term map of intuitions, failures, and directions that might become useful later.

**Experiments need to finish**

After you pick an idea, the next important thing is execution. Experiments need to be reproducible. You need to expect them to finish. And, most of the time, you should let them finish.

Stopping experiments midway is a dangerous habit. Sometimes it is necessary if the run is clearly broken, the loss explodes, or the config is wrong. But if you stop a run only because it looks slightly worse early, or because you got impatient, then you often lose the result. You cannot really use it as evidence. You cannot write it down properly.

An unfinished experiment is often not a piece of knowledge. It is just compute spent. This matters because some ideas have different behavior at different points in training. This sounds obvious but actually has some implications. For example if you see your run is better and you use cosine schedule and you stop mid-way, your run did not properly decayed, and perhaps you could've get better results at the same token horizon. If you kill too many experiments early, your taste becomes biased toward ideas that look good immediately.

Of course, you still need judgment. You should not waste compute on obviously broken runs. But the default should be that a properly launched experiment finishes, because otherwise you will not know the result. A research process should create evidence, not just curves.

**Keep a manual table**

I keep a Google Sheet with most of my runs. For me it's actually one of the most important parts of the process. When you launch enough experiments, you cannot keep everything in your head. You will forget which run used which config, which baseline it compared to, what was the conclusion etc.

This becomes especially important when experiments become numerous. If you launch 100 experiments a week, after a few weeks you will almost certainly get lost. I prefer this table to be manual, not AI-generated. The manual work of adding something to the table creates a mental connection between the experiment and the recorded result. You remember more when you write it down yourself and become more accountable to the run.

**Verify configs manually**

Another simple rule is to always verify the config manually before launch. Check how the config looks before the actual experiment starts. Ensure the fields are correct, the baseline config is the one you expect, the architecture is right (including batch size, parallelism settings, and learning rate), and the feature is actually enabled.

This becomes more important when many people contribute to the same codebase. The repo changes. Defaults change. Features get merged. Baselines get updated. Again stating the obvious, but -- pull latest main frequently!

**Baseline configs are extremely important**

Baseline configs are one of the most important parts of experimentation. When I joined, I introduced a process of baseline config and keeping it up to date. The idea is simple: we have one main baseline config in main, and we update this baseline with every new verified feature. Other team members do not have to rerun the same old baseline again. Everyone uses the same architecture and the same reference point.

Without this, research becomes messy very quickly. One person compares against one baseline. Another person compares against a slightly different one. A third person uses an old config. Someone forgets that a feature was merged. Someone else runs with a different architecture detail. After enough time, people still say "baseline," but the word does not mean one thing anymore.

The baseline should be dated. That is how you discriminate between versions. A dated baseline gives you a stable reference. You can say this experiment compares to the baseline from this date, with this architecture and this training recipe. Then, when the baseline changes, it changes explicitly.

The flow is simple. You have a baseline. Someone launches a feature run with its own name and its own date. If the feature is successful, it gets pushed to main. After a few new verified features are added, the baseline is retrained with a new date. From that point on, every new feature compares against the new baseline.

This is important because you want quality to be monotonically increasing. You want the main config to become better as verified features are added. It becomes a shared collection of things that worked, and also allows you to start a production run right away.

**Pick baseline sizes that match your pace**

We usually keep two baseline sizes: a smaller 4B MoE config and a larger 17B MoE config. The 17B MoE is the most production-like config. It is similar in sparsity to our production models and slightly more overtrained, around TPP 300, and it is something we trust. It is large enough to behave more like the models we actually care about, while still being possible to run in a normal experimentation loop. The smaller 4B MoE config is useful for faster iteration to test many ideas quickly.

Choosing these sizes is a trade-off. I intentionally pick baseline configs that can finish in a normal amount of time, usually around a day, on a reasonable amount of compute resources. This is quite important. If the baseline takes too long, nobody wants to wait. People launch fewer experiments.

So the baseline has to suit your resources and your pace. Slower pace can allow for longer training runs, while faster pace requires smaller runs that still provide signal, often through a multi-tiered approach. If you have fewer ideas, slower pace can be fine. If you have many ideas, you need faster pace. If the team is larger, you need even more discipline, because many people will be launching experiments at the same time. The baseline has to be cheap enough that people actually use it, and meaningful enough that the results are worth trusting.

**Evaluate in a stable way**

After the run finishes, the next question is how to evaluate it. For pretraining, likelihood-based benchmarks are solid options because they are stable. When you use generation-type evals, it is useful to average a few of them, for example GPQA. Then reporting an average of all evals is useful because it gives a simple reference. It makes it easier to compare many runs quickly. You still need to check the individual numbers when making a decision.

So the average gives you a quick reference, while the individual benchmarks tell you whether the result makes sense. This is also why stable baselines matter.

**Baseline is not enough**

A baseline is necessary, but it is not sufficient. It tells you whether the idea works at the tested scale. It does not fully tell you whether the idea scales.

So, from time to time, you need scaling laws. Building scaling laws is expensive. Usually it requires something like 7 to 20 runs, depending on the method and the quality of the fit you want. You do not want to build a scaling law for every new experiment. It is too complicated and not flexible enough for day-to-day iteration.

Instead, you build a scaling law occasionally, using a setup that is as close as possible to production: same data, same amount of repetitions, same sparsity as the target model size, same training recipe, same general setup. Then you build a scaling ladder across different sizes. This deserves its own separate topic, so I will not go into all the details here. The important part is that after you have the scaling law fit, you can use it to reason about whether a new method is more compute efficient.

**Efficiency gain**

Suppose you have a new run. You compare it against the same-size baseline, and the new model is better in loss. That sounds good, but how much better is enough? Is 0.01 enough? Is 0.005 enough? Is it scale dependent?

This is why raw loss differences are not always the best way to think. Of course, loss matters. In the ultimate form, better next-token prediction usually leads to better everything. Our general objective is to improve next-token prediction. But when evaluating experiments, you often need a more interpretable metric than "loss is lower by a small amount."

This is where compute efficiency gain is useful. The idea is simple. Your new model reaches some loss after spending a certain amount of compute. Call this observed compute, C_observed. Using your baseline scaling law, you predict how much compute the baseline would need to spend to reach the same loss. Call this C_predicted.

Then:

efficiency gain = C_predicted / C_observed

If the ratio is greater than 1, your method is more compute efficient than the baseline. It reached the same loss with less compute. If the ratio is below 1, it is underperforming. This is a nice numerical definition of improvement. It translates a loss difference into compute. Instead of asking whether 0.01 loss is enough, you ask how much compute the method saved relative to the baseline scaling law. This is much easier to reason about.

In practical terms, this means training a few models—often around four—at different compute budgets and sizes, then checking how the compute efficiency gain behaves across them. If the gain is consistently above 1, that's a good sign; if it increases with scale, that's even better, and if it stays stable, that's still acceptable. But if it drops as you scale up, then most likely the idea doesn't work. This is why baseline experiments and scaling experiments serve different roles: baselines are for fast iteration and early signal, while scaling checks give you confidence that the idea will hold at larger, more realistic sizes. Usually for production you want to validate both.

**Sometimes you need extra checks**

Not every experiment is only about standard pretraining loss and standard evals. Sometimes you need a new baseline. Sometimes you need to run context extension to check long context performance. In other cases, the experiment is more task-specific, or you need to understand speed separately from loss, or check whether the method changes memory, communication, or parallelism behavior.

This is why the research question at the beginning matters. The evaluation should match the question. If the question was about compute efficiency, check compute efficiency. If the question was about long context, check long context. If the question was about architecture quality, check likelihood and evals. If the question was about training speed, check actual speed in the training framework and MFU.

**Conclusion**

If you keep your ideas structured, know which parts to look at, complete your experiments, log them, verify the configs, compare against a trusted baseline, and develop research taste, you will succeed. It still takes time. It still takes effort. You still need to test many ideas. Many of them will not work. Some will work only at small scale. Some will work but not be worth the complexity. Some will point to better versions of themselves later.

But this is fine. The point is to build a setup where every experiment teaches you something. A good result becomes a feature. A bad result removes a direction. A confusing result tells you that the experiment was not designed well enough. Over time, the process improves your taste.

For me, this is the most important part of ML experimentation: not just having ideas, and not just launching runs, but actually learning from them.

### Key Quotes

> "If the experiment did not start from a question, the answer is usually ambiguous."

> "An unfinished experiment is often not a piece of knowledge. It is just compute spent."

> "After enough time, people still say 'baseline,' but the word does not mean one thing anymore."

> "A research process should create evidence, not just curves."

> "efficiency gain = C_predicted / C_observed"

### Extraction Notes

Full body recovered 2026-07-12 via Claude-in-Chrome with a logged-in x.com session (previous Playwright pass was unauthenticated and only captured the teaser). Body verbatim, including the author's grammar.
