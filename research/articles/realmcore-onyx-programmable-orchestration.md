## Designing a programmable runtime for agent orchestration (Onyx)

**Source**: https://x.com/realmcore_/status/2073170941878944022
**Author**: akira (@realmcore_), Random Labs
**Date**: 2026-07-03 (6:24 PM)
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~4,100 words (code blocks included)
**Engagement**: 16 replies, 55 reposts, 389 likes, 588 bookmarks, 858K views

---

### Summary

Introduces Onyx, a VM for programmable agent orchestration — 'a runtime that turns orchestration into software engineering.' Traces the lineage (ReAct → Ralph loop → RLM/CodeAct → Deep Research → Cursor's planner/executor harness → Codex /goal → Karpathy autoresearch → Slate live orchestration → Claude dynamic workflows) and argues all of them reach for the same thing: deterministic control over how agents execute over time, but none have binding behavioral contracts. Onyx's answer: `*.program.ts` TypeScript programs with run/spawn verbs, zod-enforced output types, declared persistent state namespaces shared by agents and code, sleep/checkpoint primitives for long-running loops, loud error semantics (agent failure = thrown exception), per-program model routing and budget caps, and import-based composition. Ten VM requirements enumerated (state, types, control flow, error handling, resource management, isolation, lifecycle, composability, visibility, durability); durability is explicitly future work.

### Relevance to dot-agents

The load-bearing article for da-recipe-scripts. dot-agents already owns the programmable-orchestration end (the Workflow JS engine). The lesson is a BOUNDARY one: recipes stay mechanical/deterministic (below skills); programmable orchestration is the Workflow engine. See Part I of the evaluation doc. The full body sharpens this: Onyx's "skill is not a binding behavioral contract — you cannot get a guarantee out of text" is the exact argument for why the full-loop orchestration runtime routes verdict gating through typed code, not prompt text; the checkpoint primitive (fixed-shape notify to the main agent) parallels the staged-runtime monitor + iter-log; state-gated subagent completion parallels merge-back contracts.

### Body

This article introduces Onyx, our VM for programmable agent orchestration. And by extension, a runtime that turns orchestration into software engineering. By the end of this article, you'll understand the constraints and design decisions that went into building the VM as well as how to create your own programs and architect your agent systems.

**Introduction**

Agents are inherently non-deterministic. That's the whole point. If you wanted determinism, you'd be writing software.

But somewhere along the way, everyone using agents collectively wanted to push them further. We learned that breaking execution into structured steps helps performance: Plan, Implement, Review, QA etc. Then we seemingly agreed to write scripts, tools, and skills to steer each agent, to share context across them, and to guardrail them. We then patch these scripts together by piping text between agents, and because we're just passing text around it sort of works.

If you spent enough time on the problem and were particularly clever, you'd have figured out how to get guarantees out of your system so that you could have conditional execution based on a given state. And you'd probably store that state in a parsable markup file or set of markup files to steer your bash scripts. You might've even built a custom cli for your agents to use.

As engineers this is familiar, we use scripts while doing software engineering. However, modern software isn't built by chaining bash scripts and cli tools. Instead, we have programming languages, runtimes, and tool chains to help us engineer our systems. We write software with programming languages because they come with a standard library, clear semantics, and an execution model we can rely on. They have rich ecosystems with toolchains for all of our needs.

The guarantees they give us about our systems allow us to reason at higher levels of abstraction.

But there's no equivalent for engineering agent systems. In order to build systems, Agent orchestration needs to be programmable in the exact same way as modern software.

Today, we are introducing the spec for PROGRAMS (*.program.ts), and Onyx, our VM built for deterministic agent orchestration. This article post explores a history of agent orchestration, the static and runtime semantics of a VM that can run a program, and it's implications for where the field is headed.

It sounds expensive, but it's actually not. I explain this later in the article.

For those who are curious, this is what Andrej Karpathy's Autoresearch looks like as a program: [figure not captured]

**Unsolved Problems in Agent Orchestration**

To understand what a runtime for agent orchestration should include, we need to understand the limitations of agents.

An llm agent can be thought of as a json stream generator, fed into a parser, which then dispatches tool calls to an environment in a loop.

Every tool call has the exact same outer schema shape but the content of this output stream is not deterministic.

The combination of determinism and non-determinism is what has made agents so valuable. They are flexible enough to chain sequences of actions in unique ways, but deterministic enough to interact with a computer through tool calls.

Composability is almost free if you are willing to let go of the requirement that the content of that stream be typed. Models are good enough to pipe text in and out within the rails that we provide them: prompts, messages, and tool calls.

This exposes a very composable interface: text

Text is a universal interface. Everything on a computer can be serialized to text even if it is just machine code. If you can have an llm input and output text through this universal interface, you get composability over text streams.

This means the reliability of your agent behavior is directly related to the consistency of the output from the model. High output variability means more erratic agent behavior.

Once you have an interface to compose pieces through, the next constraint you care about is steerability: what you want the agent to do, and how you consistently get it to do what you want.

We steer agents by shifting the distribution it samples from, in other words, prompting.

In 2022, ReAct came out and essentially pioneered agent steerability. In fact, we can go as far as to say it made agents as we know them a thing. The thinking and reasoning about a tool output before taking a subsequent step is what keeps the loop coherent.

We still needed agents to be smarter. The use of test time compute scaling, productionized by @OpenAI's O-series of models, gave model labs the ability to bake in better agent behavior [11]. Outputting more tokens before calling a tool allows the model to escape the output distribution it would have been stuck in had it been constrained on reasoning output length. You can choose to train how the model traverses its output distribution landscape, and therefore have freedom to train in more clear agentic behavior on tasks you care about.

As the context length grows unbounded, steering the agent becomes difficult and task completion becomes less likely. Even with a reasoning model, there is no guarantee of recovery, and the agent dies right there. The agent can hit its context limit, declare an early completion, get stuck in a loop, etc.

**Pulling Guarantees out of a Non-Deterministic System**

The solutions to this were varied, but one stands out: The Ralph Loop, made by @GeoffreyHuntley. [3]

He introduced the idea that you could bound agent execution, and then use those bounds to reason about task completion. This allows the Ralph Loop to do something magical: it provides something you can rely on in a non-deterministic system.

A spark of determinism.

Better to guarantee failure and progressively ratchet towards something correct, than to pull the slot machine lever one more time. This defined boundary gives you something concrete to reason about and once you can reason about the boundaries of something, you can make a system out of it.

**Fighting the limits of context length**

There's a problem though, a fresh agent loses coherence across runs, but a single agent runs out of context given enough time.

Enter RLM by @lateinteraction @a1zhang. RLM gave us a concept for how to interact with long context (i.e. an agent run) in a structured way [4]. RLM was inspired by CodeAct, a paper from 2024 that demonstrated using code to orchestrate operations [5]. The agent writes scripts that orchestrate operations inside a REPL to then retrieve an output. RLM operates in the same way with the additional caveat that it uses variables to store context and do operations on that context. It additionally allows for recursive LLM calls in the REPL. You might lose some reactivity other loops have, but you gain the ability to programmatically work with context. The key thing here is that the scripts in the REPL are ephemeral. You get a scripting runtime and context management, but there is no reusability or composability. Just write the script, run it, and it disappears. In terms of building systems, this is strictly worse than just chaining together agents and markdown files with bash scripts because you lose persistence and bounded execution.

**Moving from individual loops to scaling orchestration**

OpenAI's Deep research[6] was one of the earliest examples of a deterministic workflow that had a general execution shape or schema with small variability on a run by run basis. The way it works is by planning out a batch of queries, running them on the web, reviewing the results, and planning out the next batch of queries. Each batch probing deeper into the problem space.

Cursor took the idea of determinism much further when @wilsonzlin demonstrated a harness that orchestrated agents to build a browser. He built a bespoke harness for coordinating large amounts of work using parallel planner agents and task agents [7]. What's relevant here is that the relationship between each part of the harness is fixed. There are planners, which explore the current system state and generate tasks, and there are executors, which take tasks and implement them in parallel. There are fixed guardrails between agents and fixed channels for communicating information. To do coordination well, you need guarantees on interfaces.

**Using termination conditions for bounded execution**

In May, Codex introduced the idea of a goal which uses a verifier loop to hillclimb against some desired end state until a task is complete. You can think of this as a production ready version of the Ralph loop, built into codex. It allows you to specify an objective, and has an automated loop that executes and reviews, built in.

Karpathy's autoresearch[9] is similar to Codex's /goal and the Ralph loop. It combines the verifiable termination condition of goal with the execution bounding of a Ralph loop over iterations, allowing it to continuously drive towards a goal. It makes progress by searching the idea space iteratively improving over time.

Up to this point, all the solutions which externalize orchestration outside the agent are fixed in their execution graph shape. They run using a handwritten pattern, and have a sort of schema for the allowed shapes they can operate in. They do not adapt per task or they do not have strong guarantees over the execution graph shape period.

**Making orchestration flexible**

In March of this year, we introduced Slate, the first coding agent to use code for live subagent orchestration in the style of RLM. It is still the only well used coding agent that uses code to do live agent orchestration. In Slate, threads can be spawned, paused, resumed, and steered in real time. The main agent deeply understands how to orchestrate all of the running subagents so that you don't have to. However, similar to RLM, we were still faced with the challenge of sharing state across subagents and ephemeral scripting, which is not something you'd run into using a bash script and a markdown file.

Even still, if the model is the one doing the orchestration, how do you steer it? Do you tell it to write its orchestration code in a specific way? What do you do?

Our initial solution (as a patch before we released the Onyx runtime) was called orchestration skills [13]. The idea was simple, allow the user to supply a skill to steer how the agent approaches its orchestration. That's it. It worked alright, but it had a lot of issues.

Namely, a skill is not a binding behavioral contract. You cannot get a guarantee out of text.

This means the orchestrator did not have to follow the desired execution pattern because there was no real way to enforce it. One of the biggest benefits of the Onyx runtime is that we solved this problem.

None of the systems mentioned have binding behavioral contracts.

Well then, what if the agent could write its orchestration code to a script per task so that execution graph is foxed [sic]? This is what Claude dynamic workflows is.[10][12] In the same fashion as RLM and Slate, by writing code to orchestrate subagents, dynamic workflows allows Claude to write and save workflow shapes. This combines with /loop to be able to loop over specific patterns. It provides a declarative contract for the behavior of a set of agents. It's still not the same as writing software as it lacks things like functional composition, but you get persistence and a strong guarantee over how the task will be executed. They are dynamically written workflow scripts for a given task ad hoc.[12] And since they are persisted to disk they have one additional benefit: they can be rerun and wrapped with orchestration glue like /loop.

If you'll notice, all of the above solutions are reaching for the same thing: a deterministic way to control how agents execute over time.

This is a story we already watched unfold in software engineering as a field. We started out by gluing together disparate systems and scripting jobs, and then our languages got more flexible and powerful. We gained more and more leverage over the engineering process with stronger ecosystems allowing us to build more reliable systems at higher abstraction levels.

Right now, agents are on the same trajectory, and today we are releasing the next step along that trajectory to allow you to engineer the systems that run your agents. Programming languages often use interpreters or VMs to schedule resources automatically. This is what gives you leverage as an engineer using the language.

If a VM were to make sense for agent orchestration, you'd need a few things:

1. Persistent state management: we should be able to define state, reference it by name, persist it, and programmatically manipulate it.
2. Type guarantees. We should respect defined input and output shapes and follow them, and be able to rely on them.
3. Control flow primitives, preferably well known ones that an LLM would understand.
4. Clear structure for error handling (e.g. try-catch).
5. Resource management: defined controls over resources like agent parallelism, cost, which models are running etc.
6. Execution Isolation: A given running agent or program should be isolated from another one unless state is explicitly shared.
7. Lifecycle control: what an agent program looks like and semantics for running, cancelling, and steering. Without this, you have no clear path to cleanup and cannot control the lifecycle management.
8. Composability: Programs should compose into each other and should be callable with defined input and output types.
9. Visibility: We should be able to know what ran, when, and should be able to trace back an execution failure in the source.
10. Durability: We should have a clear model for how we can recover from crashes and resume.

Every one of these is a problem that has already been solved by programming languages decades ago. Agent orchestration is just hitting them all again for the first time.

To truly be able to write software for this, a "program.ts" program must be authored in a runtime that supports all of the above, so that we can reason about what will happen when a program doesn't work and engineer around the failure.

This is why we built Onyx. It's an agent orchestration VM designed precisely to support both persistent composable programs and an interpreted scripting layer. Here's how it works, and what a "program.ts" compatible runtime needs to support.

**Designing the runtime**

When we design a language and a runtime for that language, we need to think about the constraints we want to be able to reason about, and what we care about being easily expressible. Then we can break the resulting semantics into two categories: static semantics and runtime semantics.

Static semantics are all of the things that can be inferred about a program just by looking at it. The things that a compiler or type checker know about a given program.

Runtime semantics define what the code actually means and how the program actually runs. This includes the underlying resource allocation and scheduling mechanics.

Our goal with a runtime for agents is to turn orchestration control flow into code, and we want to make execution state persistent and typed so that we can reliably use it to steer orchestration.

**A few VM requirements**

There are 3 VM specific things we care about beyond something like normal TypeScript execution.

1. As an agent orchestration runtime, it needs to be able to orchestrate agents. This means creating them, tracking their lifecycles etc. We want the runtime to be able to run them in a blocking or non-blocking way and schedule them correctly.
2. We want control over the output shapes of agents and want strict output contract enforcement.
3. We want to have runtime control over external resources like models and cost.

**Running Agents and Programs**

To run an agent, we selected two basic verbs: run, and spawn. Run runs a blocking agent in the foreground. Spawn runs an agent in the background. This is in line with common understandings of spawn, like posix_spawn, making it easy for a model to understand our new verbs since they are conceptually in the training data. Spawn and run allow you to directly invoke agents and programs read from disk, returning enough information for an execution handle.

Run also supports a few things. It supports directly enforced output types through zod @colinhacks, and it supports direct model overrides, making it easy to write and run programs where fanning out to multiple different models for different solutions or different steps of a task makes sense.

```typescript
function run<S extends z.ZodType>(
  name: string,
  options: ...
): Promise<z.infer<S>>
```

Run allows you to directly chain subagents inline.

```typescript
// plain agent run
const out = await run({ type: "read", prompt: () => "Reply with: ok" })

// named run (string = child workflowId)
const review = await run("reviewer", {
  type: "general",
  prompt: () => "Review the diff",
})

// structured output (typed result)
const Verdict = z.object({ risk: z.enum(["low", "high"]), why: z.string() })
const v = await run({
  type: "general",
  prompt: () => "Assess risk",
  output: Verdict,
})
```

Spawn is similar to run but creates an agent in the background. Spawned subagents are not awaited and the control flow just moves ahead. Spawn is very useful for spinning up several non blocking execution agents.

```typescript
// background agent
const h = await spawn("worker", { type: "general", prompt: "Long task" })
```

**Interacting with running agents**

We want to be able to do two types of operations on running agents: steering and stopping.

A steering message is a message sent to the agent that the llm will receive while it is running to push it in a direction. This is useful for updating the agent's task context without needing to tear down the worker.

Cancellation is also important, we want to be able to actively tear down a subagent if it shouldn't be running.

Being able to run these operations from both the live REPL and a pre-authored program gives Slate its ability to orchestrate everything in real time. It can dynamically define the shape of the orchestration at runtime, or it can author and iterate on real software to do the orchestration.

Slate is able to write programs to *.program.ts files. A program file has a few things: its name (this is how Slate knows what it is), a JSDoc description, and then the actual program body. A program declaration looks like this:

```typescript
program(async (ctx) => {
  // cheap model for search — it just needs to find files
  const findings = await run("search", {
    type: "read",
    prompt: "Find all authentication-related files",
    model: "codex/gpt-4.1-mini", // uses your built in codex key
  })
})
```

Programs follow the same async execution model which allows us to run a program in both the foreground and background, and interact with it while it is running.

```typescript
// background agent
const h = await spawn("worker", { type: "general", prompt: "Long task" })
await h.notify("focus on the parser first") // steer message to the running agent
const result = await h.result() // await completion later

// fan out, then gather
const a = await spawn({ prompt: "task A" })
const b = await spawn({ prompt: "task B" })
const [ra, rb] = [await a.result(), await b.result()]

// background a program
import Audit from "deep-audit"
const ah = await spawn(Audit, { input: { pr: 42 } })
const auditResult = await ah.result()
```

**Structured output and state**

This is a primary limitation of every other system to date. State, in all other systems, is poorly externalized and is not safely isolated. If its a file on the system, you cannot guarantee no corruption. If you can, you still cannot guarantee parseability. You cannot subscribe to state changes to drive operations, and you cannot guarantee type adherence.

Remember how we wanted persistent state that was also structured and could be referenced?

State, in Onyx, is different. State namespaces are declared, directly named and persisted over time. This means a state store can be reused over and over again, allowing you to build long running agent systems with real data.

Both agents and code read the state, and the determinism that we wanted from a runtime falls out of this. Agents read state through a dedicated tool that allows them to always interact with it in a safely structured way. Agents and programs are both consumers that can be steered to modify the state which lets the runtime rely on the state object to drive orchestration.

State, and schema adherence, gate subagent completion. Because of this, state provides a unified surface for steering the whole program.

State objects can also be passed as runtime variables down into child sessions shared with the main agent. This pass by reference access throughout the agent hierarchy (which is a first of its own) allows for cross agent communication through a shared state channel.

**Long running loops**

Some programs need to function more like running systems. Take openclaw for example. You can actually represent openclaw as a program given the right primitives. For this, we use two primitives: sleep, and checkpoint.

Sleep does what you would expect, it sleeps.

Now here's the thing, let's say you want some long running task management in the background. A predefined execution graph might get stuck or break, and so it's important that the main agent is aware of the status of the program.

To support this, we introduce the checkpoint primitive.

A checkpoint can be anything, but the reason it is named checkpoint is because it notifies the main agent with a fixed shape object. This allows the main agent to track things like task progress and be notified about changes in program state directly. In turn, the main agent can then more effectively manage a running program.

Onyx supports making an agent loop like Openclaw i.e. a persistent agent with a heartbeat.

This is actually really cool, you can compose the primitives into a completely different type of agent just by using a while loop, a sleep, and a checkpoint.

Openclaw can simply be represented as a program file!

```typescript
// A program for running a long running auto-research style loop
for (let i = 0; i < maxExperiments; i++) {
  const idea = await run("propose", { ... })
  const result = await run("train", { ... })
  checkpoint({ message: `experiment ${i}`, data: { idea, result } })
  await sleep(30_000) // cool down between experiments
}

// A program for running an openclaw style persistent agent
while (true) {
  const status = await run("status_check", { ...insert cheap model here... })
  if (status.pending_tasks) {
    checkpoint({ tasks: status.pending_tasks }) // return the important state and wake the main agent up
  }
  await sleep(30_000) // cool down between experiments
}
```

**Composition**

With Onyx, Slate can write a *.program.ts for you. This persists and can (and should) be treated just like normal code. It has types that come with it out of the box, runs in a runtime stripped of runtime globals, and it's just typescript, so its composition model is just importing and calling another program.

Because it's just typescript, you get things like parallelism (Promise.all) and loops for free.

Here's how you'd import one program and use it in another:

```typescript
import Audit from "deep-audit"
program(() => {
  const ah = await spawn(Audit, { input: { pr: 42 } })
  const auditResult = await ah.result()
  const fixer = await run("fixer", ... audit output) // this would run and fix the audit program output.
})
```

**Error semantics**

Errors, in the ideal VM, are thrown loudly. These should be thrown on runtime syntax issues, agent failures, crashes, etc.

Specifically, we define orchestration errors as:

- An agent is blocked on a task
- An agent failed to complete a task
- An agent ran out of steps or budget for a task
- A program ran out of budget for a run
- The orchestration model failed to write syntactically correct code
- An illegal state modification being made

All of these specific error cases define runtime semantics. They say, "You can expect this runtime to throw, because we see an agent execution failure the same as we see an error in the code". It might seem annoying at first, but this loud failure mechanism gives you something in return: an explicit way to prepare and program around failures. So in reality it actually gives you more control, not less.

```typescript
// errors are try/catch — the same as any TypeScript program
program(async (ctx) => {
  try {
    const result = await run("risky-refactor", {
      type: "general",
      prompt: "Refactor the auth module",
      model: "claude-sonnet",
      maxSteps: 20,
    })
  } catch (err) {
    // the agent failed — but we know exactly why.
    // the trace has every tool call, every model request,
    // every state write that led here.

    // retry with a different model
    const result = await run("risky-refactor-retry", {
      type: "general",
      prompt: `The previous attempt failed: ${err.message}. Try a different approach.`,
      model: "claude-opus",
      maxSteps: 30,
    })
  }
})
```

**Model selection, budget enforcement, and BYOK**

Model selection being built in allows you to have even more precise control. The /models skill gives Slate full access to the list of available models, allowing Slate to author programs with multiple different models performing different jobs. Want Fable to be the planner, but GLM 5.2 to implement inside a deterministic harness? Sure. Want to fan out a question across Gemini, GPT 5.5, and DeepSeek? That works too.

Additionally, the runtime supports two types of config overrides for programs:

- The default global models used for agent execution
- The budget to run a program with

You can directly set a run budget to cap the spend for a given loop.

Additionally, the runtime supports using your existing OpenAI and Github Copilot subscriptions.

```typescript
program(async (ctx) => {
  // cheap model for search — it just needs to find files
  const findings = await run("search", {
    type: "read",
    prompt: "Find all authentication-related files",
    model: "codex/gpt-4.1-mini", // uses your built in codex key
  })

  // reasoning model for the hard part — it needs to think
  const plan = await run("architect", {
    type: "general",
    prompt: `Design a fix based on: ${findings.output}`,
    model: "openai/o3", // Ends up using api credits
    output: z.object({
      approach: z.string(),
      files: z.array(z.string()),
      risk: z.enum(["low", "medium", "high"]),
    }),
  })

  // mid-tier model for implementation — it just needs to edit
  const handles = await Promise.all(
    plan.files.map(f => spawn("fix-" + f, {
      type: "general",
      prompt: `Apply this fix to ${f}: ${plan.approach}`,
      model: "anthropic/claude-sonnet-5",
      maxSteps: 15,
    }))
  )
  await Promise.all(handles.map(h => h.result()))
})
```

**Defining the authoring surface**

There were two main factors in designing the authoring surface for programs: how easy it is for an agent to understand it, and how easy it is for a human to read it. We chose relatively simple verbs that read like english, and explicitly decided we wanted to model orchestration procedurally rather than declaratively.

The selection of TypeScript as a language was important as well. There is so much procedural TypeScript code in the wild that a model will implicitly understand TypeScript semantics, even without post-training.

**Engineering pieces of our software factory**

The next question to answer is: what does all of this buy you?

It buys you the ability to write actual software for your agent orchestration. You can now engineer your own agent orchestration end to end.

You can engineer the factory.

For example, you can make a program that monitors Github in a loop, and a separate program that runs an implementation agent with a QA agent for review. Both individually useful patterns you might come across in the wild. Then you can put them together to make a system that listens for comments on a PR, spawns an implementer to address those comments, and then spawns a QA agent to make sure the fix is valid.

You can then use this program hooked up to a task queue to delegate and monitor work on your codebase, and have it automatically respond to PR comments.

And you can do all of it using fast open weights models. Because it's just code, you don't need a powerful LLM to think through the orchestration after it is authored the first time.

Now for the fun part, time to share some of the programs we've used for massive output increases.

**Deep Codebase Research**

We use this program to help scope out tasks. It does deep research on the state of our monorepo, and prepares a research packet for an implementer to reference. We use it all the time. It sounds expensive, but it's actually not. You can run this program in Slate with DeepSeek V4 Flash and the research process is thorough but dirt cheap.

**Goal-Review-PR**

This one is one we use to implement a task once the research is done. Luckily, by the time the research reaches the goal program, most of the task ambiguity has been worked out, so it makes executing the task even faster. Front loading the research with a lightweight OSS model makes it easy for us to use an expensive model like Opus for what matters: writing actually good code and verifying the state of the system. You could even modify the program to use GPT 5.5 to adversarially review the work of Opus 4.8.

**Autoresearch as a Program**

Autoresearch[9] was originally entirely LLM-driven. Direct an agent at the program.md prompt and it decides what to try and how to progress.

Unsurprisingly, Autoresearch is actually just a program.

Agent programs let you invert that and put the control flow in the runtime. The program owns the control flow while agents do the side-effecting work (editing code, running git, SSH-ing to the remote GPU, training). For the autoresearch program, the keep/revert decision is deterministic code:

```typescript
kept = status === "ok" && valBpb != null && valBpb < best
```

In our case, the program runs a setup agent to prepare a fresh repo and verify the remote A100 is reachable. If setup fails, it returns early with a clean exit based on a typed value. Otherwise it enters the experiment loop.

Each experiment gets a fresh agent. The agent is given the current best config and the history of prior ideas and outcomes, so it doesn't repeat itself and can build on what was kept. It proposes a change, edits train.py, commits, rsyncs to the remote machine, trains, and classifies the result.

The agent and the program share state. The agent writes data to the state, and the program evaluates the state for control flow. Based on the outcome, a recorder agent updates results.tsv, and optionally resets the run if the program decided to throw out the experiment. This leaves the git HEAD always pointing to the current best branch of the experiment tree.

There are two core differences worth paying attention to: 1) this runs in a program so we can spawn a fresh agent per experiment and 2) we can decide what task the agent should be doing based on live program state.

And it looks like this in code:

```typescript
// ---------- Program ----------

program(async (ctx) => {
  const c = cfg(ctx.input)
  const total = ctx.input?.maxExperiments ?? 20

  const setup = await run("ar-setup", {
    prompt: setupPrompt(c),
    type: "general",
    maxSteps: 40,
    output: SetupResult,
  })
  if (!setup.ready) {
    return { aborted: true, reason: `setup failed: ${setup.note}`, setup }
  }

  let best = c.baselineValBpb
  let bestCommit = setup.baselineCommit
  const history = []

  for (let i = 1; i <= total; i++) {
    let exp
    try {
      exp = await run(`ar-exp-${i}`, {
        prompt: experimentPrompt(c, i, total, best, historyText(history)),
        type: "general",
        maxSteps: 80,
        output: ExperimentResult,
      })
    } catch (err) {
      // Agent errored/blocked — treat as a crash, restore repo to best, continue.
      exp = {
        description: `experiment ${i} agent error`,
        commit: "error",
        status: "crash",
        valBpb: null,
        peakVramMb: null,
        numSteps: null,
        exitCode: -1,
        retries: 0,
        note: String(err?.message ?? err).slice(0, 200),
      }
    }

    const kept = exp.status === "ok" && exp.valBpb != null && exp.valBpb < best

    await run(`ar-record-${i}`, {
      prompt: recordPrompt(c, exp, kept, bestCommit),
      type: "general",
      maxSteps: 20,
      output: RecordResult,
    })

    if (kept) {
      best = exp.valBpb
      bestCommit = exp.commit
    }

    history.push({
      idx: i,
      description: exp.description,
      status: exp.status,
      valBpb: exp.valBpb,
      kept,
      commit: exp.commit,
      retries: exp.retries,
    })

    await checkpoint({
      name: `experiment-${i}`,
      message: `exp ${i}/${total}: ${exp.status}${kept ? " KEPT" : ""} val_bpb=${exp.valBpb ?? "n/a"} (best=${best})`,
      data: { i, total, status: exp.status, valBpb: exp.valBpb, kept, best, bestCommit },
    })
  }

  const kepts = history.filter((h) => h.kept)
  return {
    baselineValBpb: c.baselineValBpb,
    bestValBpb: best,
    bestCommit,
    improvement: c.baselineValBpb - best,
    experimentsRun: history.length,
    kept: kepts.length,
    crashes: history.filter((h) => h.status === "crash").length,
    infraFails: history.filter((h) => h.status === "infra_fail").length,
    localRepo: c.localRepo,
    branch: c.branch,
    history,
  }
})
```

**Future work**

The only remaining VM requirement we have not defined yet is the durability model for programs. It is unclear what the correct model for resuming and handling the lifecycle of a program is, and what level of control should be exposed on the runtime.

Beyond that, there are so many exciting things we will be adding to support different workloads and task shapes, so that we can write real software to orchestrate agents better. We're certain that many of the patterns will fall out of people using programs in creative ways on their own.

We genuinely cannot wait to see what you build.

- RL Team

**References**

1. Yao et al., "ReAct: Synergizing Reasoning and Acting in Language Models," 2022
2. Geoffrey Huntley, "The Ralph Loop"
3. Geoffrey Huntley, "everything is a ralph loop," January 2026
4. Zhang, Kraska, Khattab, "Recursive Language Models," December 2025
5. Wang et al., "Executable Code Actions Elicit Better LLM Agents," ICML 2024
6. OpenAI, "Introducing Deep Research," February 2025
7. Cursor, "Scaling Agents," January 2026
8. OpenAI, "Using Goals in Codex"
9. Andrej Karpathy, "autoresearch"
10. Anthropic, "Introducing Dynamic Workflows in Claude Code"
11. OpenAI, "Learning to Reason with LLMs," September 2024
12. Anthropic, "A harness for every task: dynamic workflows in Claude Code"
13. Random Labs, "Skill Chaining"

### Key Quotes

> "a skill is not a binding behavioral contract. You cannot get a guarantee out of text."

> "Better to guarantee failure and progressively ratchet towards something correct, than to pull the slot machine lever one more time."

> "Every one of these is a problem that has already been solved by programming languages decades ago. Agent orchestration is just hitting them all again for the first time."

> "State, and schema adherence, gate subagent completion."

> "Because it's just code, you don't need a powerful LLM to think through the orchestration after it is authored the first time."

### Extraction Notes

Full body recovered 2026-07-12 via Claude-in-Chrome with a logged-in x.com session (previous Playwright pass was unauthenticated and only captured the teaser). X's text extraction flattens code blocks; line breaks/indentation inside the ```typescript``` fences were mechanically reconstructed from the flattened text (content unchanged). One inline figure (Autoresearch-as-a-program screenshot) and the reference-list numbering were not carried by the DOM text; numbering re-added by position. "foxed" is sic in the original (likely "fixed").
