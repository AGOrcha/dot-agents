## missing commands

1. workflow merge-back 
The validation agent should be calling it in the new flow as it must fill in the verification fields, (new as well), then reviewer should check the validation artifacts, and then make the decision right.

2. worflow verify record
this feels like similar to the review-decision.yaml just not as robust. wondering if it should merge and if it does how do both change. The review agent would use the synthesized version.

3. workflow checkpoint --log-to-iter 
with the split of responsibilites the agent stubs should be filled in by their respective new agent: 
```go
// Agent stubs
		Item:           "",
		ScenarioTags:   []string{},
		FeedbackGoal:   "",
		TestsAdded:     0,
		TestsTotalPass: nil,
		Retries:        0,
		ScopeNote:      "",
		Summary:        "",
		SelfAssessment: iterLogSelfAssessment{}
```
also think about other fields that can / should be auto collected from each agent, and what more fields with the split should be added to this

4. c
workflow fanout, verifier sequence
one consistency thing, there are two similar flags used in orchestrator:     --project-overlay "$worker_overlay_rel" --prompt-file "$worker_overlay_rel"
i can see the benefit of a separate prompt file but in this script copying the same one feels odd unless there's something i'm missing about the bundle content.
yup schema and cli change makes sense

Yeah make sure that all new schemas are in the write scope, basically have the schema's ready / embedded in p5? so that it can create the file or use the file and then integrate it properly into the cli tool

5. ralph-pipeline loop break
workflow orient should have a json mode, it might not be supported yet: docs/generated/GLOBAL_FLAG_COVERAGE.md. see what the approx json version would be and if would suffice, if not then lets consider our options, would prefer not to use python3 as it may not be possible on all systems, would be better if was a way to use a da native feature to get this

6. worflow fold-back phase 6
I'm starting to think it would make sense to group observations by the running task / slice that running the impl -> verifier -> reviewer -> orchestrator close, as they may be related and considered together for more whole starting point for the one looking and deciding what to do with them. but if they are not related then separate observation would be better
And yes each path should have the command format for create or update available (update if we go this route want decision on this before writing plan)

7. New source type + external config support
Lets think through how sources are setup now how we use them, and how we should be consuming specfic sub resources from a source, and what the current needs demand, and brainstorm how to design schema and implementation for future evolution

---
## schema gaps
yeah as mentioned above we should be making these changes and should be same task as feature

---
## task dependency gap
makes sense yeah add that

---
## workflow notes:

### identity conflict:
yeah we're refactoring and enhancing the loop-worker single subagent's responsibilites to separate subagents. orchestrator skill and prompts would need to be make aware as well this seems missing. 
Skills don't make as much sense for the impl-agent (agent definition like ~/.agents/agents/dot-agents/loop-worker/AGENT.md) maybe more so for the different app_type information / profiles, which the impl-agent definition could have in it's instructions so it knows to use the corresponding one, the verifier sequence would be the same effective one for the impl-agent but instead of senior sdet - verifier instructions, would be senior developer level - development instructions. Reviewer should be two phase one as a broad level review to ensure domain stability and to see if things are moving in the right path, second is tech lead review to ensure archtectural standards and decisions are implemented properly and all present. 
Since it would be akin to the patterned workflow we have to maintain standards, prioritize and define spec with business -> plan technical implementation, divide and orchestrate work to team members, implement, review, test, release. The one step we're 'adding' in is implicit by a human developer during / after implementation. I'm thinking to have 'focused unit' mandatory during / after implementation. Andother verifications after by dedicated agent.

### parallel verifier isolation
i'm thinking that it might be worth it to just use worktrees to manage isolation. This is a tradeoff as adds a task to the orchestrator to review the changes and integrate / merge them back. Each worktree would need to go through the proper PR cycle back to orchestrator branch and the orchestrator would have to ensure the workers after commit ensure their build passes, (the normal version would as well and as soon as it's pushed to the remote the CI is triggered / on pr create, update it's also triggered). That does require more thought to make it sound so lets keep that out of scope for this plan, simple pattern as discussed can be used for now: so likely first step would be to limit tests to write scope so that task's units test cases' file(s) should be used during development do focused runs during the implementation., the CI as mentioned will run the full configured suite. 

### Verification directory lifecycle
So with the impl-agent running unit tests it should add it's result. The verifier agent should add it's artifact by the type it is. the review agent should read the results, the acceptance path is simpler and feels more thought through. It should be cleaned up in delegation closeout if accepted. 
The deny paths are a little more involved, first one would be verifier deny immediate send back.

### ralph run loop
yeah good catch. I'm thinking to make also allow multiple ids into the filter if wanted.

### playwright for parallel api/ui_e2e verifiers 
agent notes are good, also thinking how eventually build a regression suite based off of tests used to verify at develop time. there should be some sort of mechanism in the background to collect analyze, build / update the suite, run, analyze the run. might be worth keeping as a future idea and keeping it out of scope for this plan since i want to flush out other potential daemon processes as well.

### Test cases and higher level tests
Following TDD we will create the tests before implmentation, against what the acceptance criteria for a task lays out. This will involve creating new for cases that are not covered yet, update existing ones if they fail to compile / data scenario changed, and removing if the existing one's business scenario is out of date.
levels of tests (unit, and verifier ran) should both follow this principle with each slightly more tailored to its purpose. Unit test should absolutely have business scenario tests to be ran, not just the verifier ran suites.