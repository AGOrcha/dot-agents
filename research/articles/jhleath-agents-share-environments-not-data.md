## Agents share environments, not data

**Source**: https://x.com/jhleath/status/2045575156815523999
**Author**: Hunter Leath (@jhleath)
**Date**: 2026-04-18
**Method**: Playwright
**Word count**: ~900 words

---

### Summary

Hunter Leath of Archil argues that multi-agent systems should share the entire environment (the disk/file system) rather than copying data between agents via S3 uploads. Drawing a parallel to how microservices passed S3 URLs instead of raw data in 2015–2025, he proposes that agents abstract the file system as a server — enabling constant-time hand-off of full context (files, binaries, SQLite tables) to any other agent anywhere in the world via a portable `diskId`.

---

### Body

Agents today mirror the software development world of the late 2000s before the secrets from Google, Amazon, and Facebook on scaling engineering teams and services became common knowledge. Today, agents are monolithic and the data that they work is (often) small.

Now, let's run a though experiment. Imagine that it's 2017 and your organization has built a system out of microservices that's processing large pieces of data that's coming in from users (>1 GB each). How do you pass this to your microservices?

The data is too big to stuff in a SQL database, so you're almost certainly uploading the data to S3, keeping the URL handy, and putting the URL into your SQL database or sending it through your Kafka cluster.

You're sharing pointers, not data.

This was the backbone of designing systems from 2015 to 2025 and it put S3 at the center of it all. The unit of work was one object. You ingested it, you uploaded it, and you processed it.

Fast-forward to 2026, and now, everything is about building agents. The interesting thing about agents is that they look super similar to what we were doing before.

Imagine that you're an AI startup working in the legal field with a recognizable actor as your spokesperson. Your users are uploading document after document of legal information into your system so that your agents can learn from it.

There's a big difference than the systems of the past, though. We used to operate on the scale of an individual document. Now, these agents create value by discovering the relationships between documents and using specialized tools to extract information from them.

The agent isn't operating on individual pieces of data, it's operating on an entire environment — its context — if you will. Each piece of data isn't useful in isolation, the value only comes in seeing the entire customer's environment together.

Now, today, the way that people are trying to manage this remains almost the same as what we were doing before. I see developers desperately trying to zip up entire file systems and upload them to S3, or push all the data into a single SQLite document.

This works for some people right now, because the scale of the data is small and the agents are monolithic — single-player. But, it's reasonable to believe that the arc of agents is going to follow the arc of software in the 2010s. Everything is going to get bigger. More data. More teams. More coordination.

How should we think about the "micro-agent" from one team handing off data to the micro-agent from another team?

Well, we need to give the second agent everything. All of the documents, all of the tools, all of the cookies, all of the sqlite files, all of the information on progress that's been made before, everything. Otherwise, we can't expect it to be able to create its own insights.

At Archil, we've been thinking deeply about this as: **share the environment, not the data.**

It's not feasible to be able to upload all of this data to somewhere in S3 so that it can be downloaded to another agent, configure all the sandboxes it uses, and only then let it start running.

Instead, the environment of the agent is the disk (the file system) that it's using.

The disk contains: the specialized binaries that you install, all of the documents for each user, the ability to easily derive linkages between those documents, and any structured data in the form of SQLite tables associated with the user.

How do you go about sharing this without spending a ton of time uploading and downloading the disk to S3?

Well, there really wasn't a good solution to this before Archil's Serverless Execution.

You see, data has gravity. If you store your file system on a local disk, then if you want to access it later, you have to ... do your work on that same local disk. This isn't great for resource usage, or the ability to share the entire environment across teams running different agents.

If you abstract the file system a level out — as a server that accepts commands to a Linux machine and returns results — then suddenly that service can respond in near-constant time regardless of where the requests are coming from — similar to a SQL database.

How does this work in practice? You make a bash tool using Archil with code that looks like this:

```typescript
const bash = tool({
  description: "Run a shell command inside the workdir.",
  inputSchema: z.object({ command: z.string() }),
  execute: async ({ command }) => {
    const { stdout, stderr, exitCode } = await disk.exec(command);
    return `exit ${exitCode}\n${stdout}${stderr ? `\n${stderr}` : ""}`;
  },
});
```

Now, here's the cool part — what if your agent could just share that tool (with exactly the same files on the disk) directly to any other agent that you're working with? Think of it like a meta-function that looks like this, you give a diskId, and you get out a bash tool — anywhere in the world.

```typescript
const buildBashTool = (disk) => tool({
  description: "Run a shell command inside the workdir.",
  inputSchema: z.object({ command: z.string() }),
  execute: async ({ command }) => {
    const { stdout, stderr, exitCode } = await disk.exec(command);
    return `exit ${exitCode}\n${stdout}${stderr ? `\n${stderr}` : ""}`;
  },
});

buildBashTool(customerDisk1)
```

This is **insanely** powerful for building specialized agents that can hand-off context between each other in constant-time, no matter where in the world they are located.

We think of this as "agent hand-off". We expect that the next-generation of agents are going to be multi-player, working on larger data sets than we have even considered today, and that the ability to fully hand-off the context from one agent to another is a critical component in how these applications will be built.

If you're interesting in playing with this, you can try out Serverless Execution on Archil today by spinning up a disk at https://console.archil.com or "npx disk create".

---

### Key Quotes

> "You're sharing pointers, not data."

> "At Archil, we've been thinking deeply about this as: share the environment, not the data."

> "Data has gravity. If you store your file system on a local disk, then if you want to access it later, you have to ... do your work on that same local disk."
