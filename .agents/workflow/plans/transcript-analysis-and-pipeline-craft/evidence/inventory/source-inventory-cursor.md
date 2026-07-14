# Source Inventory — Cursor

Rubric: `../../methodology/evidence-rubric.md` §1. One row per session (session directory/file).
Timestamps are first/last **record** timestamps (never file mtime). Subagent transcripts are folded
into their parent session (see Exclusions); `record_count` counts the **primary** `.jsonl` only.

- **Corpus path:** `~/.cursor/projects/<slug>/agent-transcripts/<uuid>/<uuid>.jsonl (primary) + subagents/**.jsonl`
- **Sessions:** 158  |  **folded subagent transcripts:** 44  |  **total `.jsonl` files:** 202
- **Date range:** — (no in-record timestamps)
- **Coverage:** tokens 0.0% · cost 0.0% · wallclock 0.0%
- **Status:** {'complete': 156, 'cutoff': 2}  |  **Sensitivity:** {'internal': 144, 'public-ok': 12, 'sensitive': 2}
- **Models seen (union, primary+subagents):** —

| evidence_id | project_slug | started_at | ended_at | records | subs | tokens | cost | wallclock | model(s) | status | sensitivity |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `cursor:private-tmp-payout-submodule-backup-client-se/29d74efd-17c9-4be9-86ba-e68daa8543bf:29d74efd-17c9-4be9-86ba-e68daa8543bf` | private-tmp-payout-submodule-backup-client-se | — | — | 4 | 0 | n | n | n | — | complete | internal |
| `cursor:private-tmp-payout-submodule-backup-client-se/6ec974a3-5254-4662-85e4-da99ce1c43c7:6ec974a3-5254-4662-85e4-da99ce1c43c7` | private-tmp-payout-submodule-backup-client-se | — | — | 15 | 0 | n | n | n | — | complete | internal |
| `cursor:private-tmp-payout-submodule-backup-client-se/e0db1a11-fbdd-4741-83e7-9053f1ceef7c:e0db1a11-fbdd-4741-83e7-9053f1ceef7c` | private-tmp-payout-submodule-backup-client-se | — | — | 4 | 0 | n | n | n | — | complete | public-ok |
| `cursor:private-tmp-payout-submodule-backup-client-se/f6e95467-3760-45b1-8f5d-f3c39a1d0c26:f6e95467-3760-45b1-8f5d-f3c39a1d0c26` | private-tmp-payout-submodule-backup-client-se | — | — | 23 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-ResumeAgent/d706fa5f-aff6-4dfe-8a51-5d14e60d2b3b:d706fa5f-aff6-4dfe-8a51-5d14e60d2b3b` | ~-Documents-ResumeAgent | — | — | 80 | 6 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/42cc1e58-12e9-4a82-91ed-792ae39d5350:42cc1e58-12e9-4a82-91ed-792ae39d5350` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 24 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/48e4a89a-2bb4-4ff3-8c82-a1f42ea2cadb:48e4a89a-2bb4-4ff3-8c82-a1f42ea2cadb` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 57 | 2 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/4b6981fa-2cc6-41ce-a3d8-d7edb065c310:4b6981fa-2cc6-41ce-a3d8-d7edb065c310` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 44 | 3 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/5070c327-8bb1-47ac-8de0-1ffb6e17c47d:5070c327-8bb1-47ac-8de0-1ffb6e17c47d` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 31 | 3 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/560f9e74-52e1-4e2a-8748-129710aacbc4:560f9e74-52e1-4e2a-8748-129710aacbc4` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 104 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/6dc9c16d-d72c-4607-a6ac-ea97e5395461:6dc9c16d-d72c-4607-a6ac-ea97e5395461` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 62 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/6dfe4780-f5f4-4e52-8e84-c1031b8bc512:6dfe4780-f5f4-4e52-8e84-c1031b8bc512` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 69 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/710f07b5-8835-4881-94bb-b25fa2fb48c4:710f07b5-8835-4881-94bb-b25fa2fb48c4` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 40 | 2 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/830f6693-0b38-471c-8b99-13e754db7bde:830f6693-0b38-471c-8b99-13e754db7bde` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 4 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/878a6961-8534-4ab2-bc8d-993a2edd628c:878a6961-8534-4ab2-bc8d-993a2edd628c` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 38 | 4 | n | n | n | — | cutoff | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/8a92b83c-f809-4ca4-80b7-eda8c1b4aa49:8a92b83c-f809-4ca4-80b7-eda8c1b4aa49` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 35 | 1 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/ab5770c3-9652-4850-8bef-4ecf9bdbce3a:ab5770c3-9652-4850-8bef-4ecf9bdbce3a` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 49 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/ae2cae87-ee40-4aa5-ba81-7f66da40e95b:ae2cae87-ee40-4aa5-ba81-7f66da40e95b` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 3 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/b52f72d7-5a0e-46b2-9e9a-5bac820f9611:b52f72d7-5a0e-46b2-9e9a-5bac820f9611` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 50 | 1 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/b8f663c2-4afc-470f-b8e6-cc5eba666879:b8f663c2-4afc-470f-b8e6-cc5eba666879` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 92 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/bb98a2ae-b9f7-4c9d-8d18-e6e693c66df9:bb98a2ae-b9f7-4c9d-8d18-e6e693c66df9` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 50 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/c8104c20-31ad-4ff0-961a-adbc7fc21e54:c8104c20-31ad-4ff0-961a-adbc7fc21e54` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 16 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/d6a77811-6a33-471b-9d2a-9cad1e8ea431:d6a77811-6a33-471b-9d2a-9cad1e8ea431` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 11 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-dot-agents-dot-agents-code-workspace/f18575e8-1e87-41ac-b287-661fc0ac00b8:f18575e8-1e87-41ac-b287-661fc0ac00b8` | ~-Documents-dot-agents-dot-agents-code-workspace | — | — | 33 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-pattern-e-20260414-223713/1aff5366-7352-49e5-a1b3-2a1fa4981a34:1aff5366-7352-49e5-a1b3-2a1fa4981a34` | ~-Documents-dot-agents-pattern-e-20260414-223713 | — | — | 48 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-pattern-e-20260415-195750/c361dcf1-8527-4c3e-8fd6-40ec28462505:c361dcf1-8527-4c3e-8fd6-40ec28462505` | ~-Documents-dot-agents-pattern-e-20260415-195750 | — | — | 41 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-script-20260414-223713/43ef318e-2e54-4825-966c-4a374ff3f827:43ef318e-2e54-4825-966c-4a374ff3f827` | ~-Documents-dot-agents-script-20260414-223713 | — | — | 4 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-script-20260414-223713/aebcd7c1-5d93-4b29-87c8-aa8b9559b4ce:aebcd7c1-5d93-4b29-87c8-aa8b9559b4ce` | ~-Documents-dot-agents-script-20260414-223713 | — | — | 7 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-script-20260415-195750/0f185c98-08e1-4d14-a1e2-2a38639af719:0f185c98-08e1-4d14-a1e2-2a38639af719` | ~-Documents-dot-agents-script-20260415-195750 | — | — | 53 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-script-20260415-195750/b9ca59a2-2641-4737-9651-3e809ae3f046:b9ca59a2-2641-4737-9651-3e809ae3f046` | ~-Documents-dot-agents-script-20260415-195750 | — | — | 7 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents-script-20260415-195750/d8ca9adc-07b6-41a6-9ecd-caad5d1aaf08:d8ca9adc-07b6-41a6-9ecd-caad5d1aaf08` | ~-Documents-dot-agents-script-20260415-195750 | — | — | 7 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/0014207d-b46b-46b3-9a0e-bd4b1f8f491e:0014207d-b46b-46b3-9a0e-bd4b1f8f491e` | ~-Documents-dot-agents | — | — | 46 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/006bd574-7e89-498d-9094-18d32e62604d:006bd574-7e89-498d-9094-18d32e62604d` | ~-Documents-dot-agents | — | — | 18 | 3 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/00fc269a-4a2e-4c9f-b691-f30e301823e3:00fc269a-4a2e-4c9f-b691-f30e301823e3` | ~-Documents-dot-agents | — | — | 9 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/02470484-b97a-4b33-80b8-b7a76a8cbb6c:02470484-b97a-4b33-80b8-b7a76a8cbb6c` | ~-Documents-dot-agents | — | — | 4 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/04358fe5-c121-4495-9dcc-4f4a377c09fa:04358fe5-c121-4495-9dcc-4f4a377c09fa` | ~-Documents-dot-agents | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/0518663b-e514-447b-afca-b52fc6bec794:0518663b-e514-447b-afca-b52fc6bec794` | ~-Documents-dot-agents | — | — | 44 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/060885dc-6b79-4933-8ae3-cb45e97b1d24:060885dc-6b79-4933-8ae3-cb45e97b1d24` | ~-Documents-dot-agents | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/07c058b8-2b4a-4b7e-9bc7-e86e5adedf71:07c058b8-2b4a-4b7e-9bc7-e86e5adedf71` | ~-Documents-dot-agents | — | — | 55 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/1b44e250-e5e2-447c-ae46-e52071da492b:1b44e250-e5e2-447c-ae46-e52071da492b` | ~-Documents-dot-agents | — | — | 149 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/1e97417c-aecf-4a2c-bf81-e1dba8e5f04d:1e97417c-aecf-4a2c-bf81-e1dba8e5f04d` | ~-Documents-dot-agents | — | — | 59 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/23371bb3-9efd-4ba9-b594-b9b0e15be9b3:23371bb3-9efd-4ba9-b594-b9b0e15be9b3` | ~-Documents-dot-agents | — | — | 78 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/2469d9c7-4023-4dd4-970a-e910a59acaf4:2469d9c7-4023-4dd4-970a-e910a59acaf4` | ~-Documents-dot-agents | — | — | 56 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/26e78436-131b-4d82-a90a-22593215dcbd:26e78436-131b-4d82-a90a-22593215dcbd` | ~-Documents-dot-agents | — | — | 87 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/2880b770-97a5-47bb-8d85-6a902e445e87:2880b770-97a5-47bb-8d85-6a902e445e87` | ~-Documents-dot-agents | — | — | 102 | 2 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/28b07314-15fe-447c-b66a-f47e25c7aad6:28b07314-15fe-447c-b66a-f47e25c7aad6` | ~-Documents-dot-agents | — | — | 7 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/32b13ca4-e3c7-4728-a8a5-7f4374cddd4e:32b13ca4-e3c7-4728-a8a5-7f4374cddd4e` | ~-Documents-dot-agents | — | — | 43 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/34f82c72-5c54-4c7f-8f17-0b0f42842de4:34f82c72-5c54-4c7f-8f17-0b0f42842de4` | ~-Documents-dot-agents | — | — | 62 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/35079e30-e967-4e97-b500-1e0d7f4c9ef9:35079e30-e967-4e97-b500-1e0d7f4c9ef9` | ~-Documents-dot-agents | — | — | 74 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/3a0e3b8d-63d2-4870-aa00-50388f823e2f:3a0e3b8d-63d2-4870-aa00-50388f823e2f` | ~-Documents-dot-agents | — | — | 3 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/3ac340f5-2ffc-4cf2-8f35-fdbe6c35846d:3ac340f5-2ffc-4cf2-8f35-fdbe6c35846d` | ~-Documents-dot-agents | — | — | 38 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/3aef927b-ddc9-4713-90e7-ad8d00c39fde:3aef927b-ddc9-4713-90e7-ad8d00c39fde` | ~-Documents-dot-agents | — | — | 22 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/3b5dda45-f4b6-4abe-84ee-1768868d6f2a:3b5dda45-f4b6-4abe-84ee-1768868d6f2a` | ~-Documents-dot-agents | — | — | 2 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/3bd105d1-0f7f-4376-8536-9d471dd8dc4f:3bd105d1-0f7f-4376-8536-9d471dd8dc4f` | ~-Documents-dot-agents | — | — | 63 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/42a730a6-a85d-46fc-a954-24aad191efb7:42a730a6-a85d-46fc-a954-24aad191efb7` | ~-Documents-dot-agents | — | — | 42 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/45e4c97c-698c-44a2-a949-5fda8497c998:45e4c97c-698c-44a2-a949-5fda8497c998` | ~-Documents-dot-agents | — | — | 113 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/46f9657b-26ab-4d1d-872e-aec2293b674e:46f9657b-26ab-4d1d-872e-aec2293b674e` | ~-Documents-dot-agents | — | — | 94 | 3 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/46f9df13-ad80-4a50-adf1-df1feee47274:46f9df13-ad80-4a50-adf1-df1feee47274` | ~-Documents-dot-agents | — | — | 26 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/47e62b6c-3e06-48d9-9916-468773be2bf3:47e62b6c-3e06-48d9-9916-468773be2bf3` | ~-Documents-dot-agents | — | — | 7 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/49dee2b6-1384-49f4-875a-3acf2cd68988:49dee2b6-1384-49f4-875a-3acf2cd68988` | ~-Documents-dot-agents | — | — | 5 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/4acb0439-aa64-4369-9f0c-a21e8c2db013:4acb0439-aa64-4369-9f0c-a21e8c2db013` | ~-Documents-dot-agents | — | — | 52 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/4afea9be-d24a-4c41-b353-f4738a501702:4afea9be-d24a-4c41-b353-f4738a501702` | ~-Documents-dot-agents | — | — | 113 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/4b6ed779-dfcf-467f-b220-7352a7555653:4b6ed779-dfcf-467f-b220-7352a7555653` | ~-Documents-dot-agents | — | — | 93 | 1 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/575f92f0-685d-4d4c-bf16-e93db45fb19f:575f92f0-685d-4d4c-bf16-e93db45fb19f` | ~-Documents-dot-agents | — | — | 32 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/5b6f50d7-8506-4625-9cf2-1798abba4069:5b6f50d7-8506-4625-9cf2-1798abba4069` | ~-Documents-dot-agents | — | — | 58 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/5c42318e-7b35-4022-bd0a-f59578b8b1e0:5c42318e-7b35-4022-bd0a-f59578b8b1e0` | ~-Documents-dot-agents | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/5f373a0b-0086-44c6-ade8-e6d6503ff8e3:5f373a0b-0086-44c6-ade8-e6d6503ff8e3` | ~-Documents-dot-agents | — | — | 8 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/5f7abbb7-9cc0-4a30-a8e5-2a24d126e837:5f7abbb7-9cc0-4a30-a8e5-2a24d126e837` | ~-Documents-dot-agents | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/613abf93-4da7-40fc-9ce5-0f04be9f6786:613abf93-4da7-40fc-9ce5-0f04be9f6786` | ~-Documents-dot-agents | — | — | 46 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/62c0ab7c-e75e-4287-8580-8fbc4399cc4a:62c0ab7c-e75e-4287-8580-8fbc4399cc4a` | ~-Documents-dot-agents | — | — | 8 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/64093e38-7ba7-43d7-a953-db4958952b53:64093e38-7ba7-43d7-a953-db4958952b53` | ~-Documents-dot-agents | — | — | 56 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/684414e2-90fa-472c-be6c-37c8e61132e6:684414e2-90fa-472c-be6c-37c8e61132e6` | ~-Documents-dot-agents | — | — | 4 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/6b2a7158-7406-4e61-ab13-15d67a43a8b0:6b2a7158-7406-4e61-ab13-15d67a43a8b0` | ~-Documents-dot-agents | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/78f5d7f0-97de-49d4-a1e9-c9bd889f3dac:78f5d7f0-97de-49d4-a1e9-c9bd889f3dac` | ~-Documents-dot-agents | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/7b3b64e3-9fcf-41a9-b464-f21c179bacef:7b3b64e3-9fcf-41a9-b464-f21c179bacef` | ~-Documents-dot-agents | — | — | 4 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/7b577d59-5355-41b4-94f2-efc3ac0dd5f1:7b577d59-5355-41b4-94f2-efc3ac0dd5f1` | ~-Documents-dot-agents | — | — | 33 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/7be3c6cc-f3c7-4def-96e9-82ae7aee272e:7be3c6cc-f3c7-4def-96e9-82ae7aee272e` | ~-Documents-dot-agents | — | — | 88 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/85443688-9252-4b9a-9bcf-1b94da0e156c:85443688-9252-4b9a-9bcf-1b94da0e156c` | ~-Documents-dot-agents | — | — | 15 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/8640fe85-dcfa-4e6f-a938-69c60ad7192b:8640fe85-dcfa-4e6f-a938-69c60ad7192b` | ~-Documents-dot-agents | — | — | 44 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/869316af-1f0e-4822-9719-81a3c4aa8f6e:869316af-1f0e-4822-9719-81a3c4aa8f6e` | ~-Documents-dot-agents | — | — | 8 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/88360e35-2cda-459a-b258-10fa7c7fee94:88360e35-2cda-459a-b258-10fa7c7fee94` | ~-Documents-dot-agents | — | — | 29 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/8c08e571-ac9c-489c-a24e-ba611b9858b7:8c08e571-ac9c-489c-a24e-ba611b9858b7` | ~-Documents-dot-agents | — | — | 101 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/8f5f7f3e-355d-483c-8835-616e86fae103:8f5f7f3e-355d-483c-8835-616e86fae103` | ~-Documents-dot-agents | — | — | 16 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/8f9a5374-5ef1-4563-8f21-628c5cee162e:8f9a5374-5ef1-4563-8f21-628c5cee162e` | ~-Documents-dot-agents | — | — | 72 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/90766da8-9063-46b0-808f-410f93a9242e:90766da8-9063-46b0-808f-410f93a9242e` | ~-Documents-dot-agents | — | — | 44 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/935b0ca7-e2d4-4048-aaa3-070a5647534f:935b0ca7-e2d4-4048-aaa3-070a5647534f` | ~-Documents-dot-agents | — | — | 46 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/967c3251-37b4-44fa-ab27-e4d992aabe0d:967c3251-37b4-44fa-ab27-e4d992aabe0d` | ~-Documents-dot-agents | — | — | 59 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/9b24e542-5dc9-407c-9472-9f55f6d7bfff:9b24e542-5dc9-407c-9472-9f55f6d7bfff` | ~-Documents-dot-agents | — | — | 81 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/9cfcfbfe-ba04-4e34-9fc0-b70c631959f4:9cfcfbfe-ba04-4e34-9fc0-b70c631959f4` | ~-Documents-dot-agents | — | — | 43 | 3 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/9d0b813c-770a-43a3-986e-61dbc49c7cfa:9d0b813c-770a-43a3-986e-61dbc49c7cfa` | ~-Documents-dot-agents | — | — | 8 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/a1981b97-f667-484e-b80d-db29916627b5:a1981b97-f667-484e-b80d-db29916627b5` | ~-Documents-dot-agents | — | — | 11 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/a1be22c8-00de-43e9-a173-71f14982711f:a1be22c8-00de-43e9-a173-71f14982711f` | ~-Documents-dot-agents | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/a2563484-e231-402c-95d7-e862a7f5d6bd:a2563484-e231-402c-95d7-e862a7f5d6bd` | ~-Documents-dot-agents | — | — | 16 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/a551024f-8939-4f4c-8c70-8bd246661edd:a551024f-8939-4f4c-8c70-8bd246661edd` | ~-Documents-dot-agents | — | — | 241 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/a84f1e7b-9ad0-434f-9538-9b6ac080ee85:a84f1e7b-9ad0-434f-9538-9b6ac080ee85` | ~-Documents-dot-agents | — | — | 73 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/ac89434f-fb6f-410a-82db-4aa4b79396a7:ac89434f-fb6f-410a-82db-4aa4b79396a7` | ~-Documents-dot-agents | — | — | 54 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/ad56b281-9e67-4bf3-9e7d-a3450d2d6049:ad56b281-9e67-4bf3-9e7d-a3450d2d6049` | ~-Documents-dot-agents | — | — | 7 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/af3e7ba8-7ea6-4e2c-8b22-22a3b3ccd854:af3e7ba8-7ea6-4e2c-8b22-22a3b3ccd854` | ~-Documents-dot-agents | — | — | 31 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/b87b6368-6b3f-4178-a480-85e8f0ccb6a1:b87b6368-6b3f-4178-a480-85e8f0ccb6a1` | ~-Documents-dot-agents | — | — | 48 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/ba7554ac-aba2-4953-b4f2-218512ec3c7b:ba7554ac-aba2-4953-b4f2-218512ec3c7b` | ~-Documents-dot-agents | — | — | 3 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/bbad1924-c9e6-40df-bb1f-f00d096f10ac:bbad1924-c9e6-40df-bb1f-f00d096f10ac` | ~-Documents-dot-agents | — | — | 120 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/bf5706ab-be1f-4e9d-8b74-47d32e52148e:bf5706ab-be1f-4e9d-8b74-47d32e52148e` | ~-Documents-dot-agents | — | — | 2 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-dot-agents/c357e7cf-1026-4786-817e-116929d73cfc:c357e7cf-1026-4786-817e-116929d73cfc` | ~-Documents-dot-agents | — | — | 38 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/c58fd98a-00da-4ec8-ad9c-6017cd89fc74:c58fd98a-00da-4ec8-ad9c-6017cd89fc74` | ~-Documents-dot-agents | — | — | 102 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/ca1ed094-51a1-4973-966e-233e902e8f25:ca1ed094-51a1-4973-966e-233e902e8f25` | ~-Documents-dot-agents | — | — | 102 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/d5f23738-b67e-4c39-a460-cf02e34ee341:d5f23738-b67e-4c39-a460-cf02e34ee341` | ~-Documents-dot-agents | — | — | 8 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/d6ac1225-6b3f-473d-b76d-93288a1bd3d4:d6ac1225-6b3f-473d-b76d-93288a1bd3d4` | ~-Documents-dot-agents | — | — | 33 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/dd7858be-fd8c-4ade-8700-65a3e3523ddb:dd7858be-fd8c-4ade-8700-65a3e3523ddb` | ~-Documents-dot-agents | — | — | 61 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/ddb7a4d5-d8a4-4f1a-880b-6abaf2cc54e4:ddb7a4d5-d8a4-4f1a-880b-6abaf2cc54e4` | ~-Documents-dot-agents | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/df428b91-ac47-47aa-9cc1-434599d6f9f1:df428b91-ac47-47aa-9cc1-434599d6f9f1` | ~-Documents-dot-agents | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/e12d7f39-a86f-484d-9f1f-248976ba7bd1:e12d7f39-a86f-484d-9f1f-248976ba7bd1` | ~-Documents-dot-agents | — | — | 56 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/e71a87f0-2d14-4eaf-ad6c-5f6ea42a447e:e71a87f0-2d14-4eaf-ad6c-5f6ea42a447e` | ~-Documents-dot-agents | — | — | 45 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/e8bc204c-d008-4d34-b7fd-47bf21566a6b:e8bc204c-d008-4d34-b7fd-47bf21566a6b` | ~-Documents-dot-agents | — | — | 56 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/eec7d15c-1b25-473d-87a7-94cf7bc5753d:eec7d15c-1b25-473d-87a7-94cf7bc5753d` | ~-Documents-dot-agents | — | — | 120 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/f3f972cb-a7df-4474-a102-41fc4470ae4a:f3f972cb-a7df-4474-a102-41fc4470ae4a` | ~-Documents-dot-agents | — | — | 87 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/f85c1e2f-17c2-4f49-b517-0696ff1225c6:f85c1e2f-17c2-4f49-b517-0696ff1225c6` | ~-Documents-dot-agents | — | — | 74 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/f8804a21-259a-4605-af12-071c630e6a39:f8804a21-259a-4605-af12-071c630e6a39` | ~-Documents-dot-agents | — | — | 141 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/f8a214bd-9837-43df-8c8d-b3e67e8ad1de:f8a214bd-9837-43df-8c8d-b3e67e8ad1de` | ~-Documents-dot-agents | — | — | 28 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/f9e38176-5af7-4f94-9251-cd0c64f94693:f9e38176-5af7-4f94-9251-cd0c64f94693` | ~-Documents-dot-agents | — | — | 41 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/fc0d31a1-3e50-4193-8ad3-3c92c2eaa8d6:fc0d31a1-3e50-4193-8ad3-3c92c2eaa8d6` | ~-Documents-dot-agents | — | — | 10 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-dot-agents/fc91ef29-3cda-4cbc-9174-be018243ffae:fc91ef29-3cda-4cbc-9174-be018243ffae` | ~-Documents-dot-agents | — | — | 66 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-client-ui/ad326685-67df-4d5b-9432-30478680f4ae:ad326685-67df-4d5b-9432-30478680f4ae` | ~-Documents-payout-client-ui | — | — | 4 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-payout-code-workspace/38b86b99-41e6-4b52-a0f9-6f2747a3d853:38b86b99-41e6-4b52-a0f9-6f2747a3d853` | ~-Documents-payout-payout-code-workspace | — | — | 41 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-payout-code-workspace/5e686b32-67cc-4b40-a418-2ab57e54d03f:5e686b32-67cc-4b40-a418-2ab57e54d03f` | ~-Documents-payout-payout-code-workspace | — | — | 71 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-payout-code-workspace/6c1f7726-dfcc-4ca6-9b75-42a0f41a1841:6c1f7726-dfcc-4ca6-9b75-42a0f41a1841` | ~-Documents-payout-payout-code-workspace | — | — | 28 | 0 | n | n | n | — | cutoff | internal |
| `cursor:~-Documents-payout-payout-code-workspace/8672247c-6792-4cf0-8e88-b498d46be0a4:8672247c-6792-4cf0-8e88-b498d46be0a4` | ~-Documents-payout-payout-code-workspace | — | — | 41 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-payout-code-workspace/9062a536-0c01-4847-bb03-aa4998732539:9062a536-0c01-4847-bb03-aa4998732539` | ~-Documents-payout-payout-code-workspace | — | — | 15 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-payout-code-workspace/9948dc94-b1bc-41de-9d05-c3212f23de8f:9948dc94-b1bc-41de-9d05-c3212f23de8f` | ~-Documents-payout-payout-code-workspace | — | — | 7 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-payout-payout-code-workspace/aa184e02-8a06-4993-adb7-21bbe70d3f7c:aa184e02-8a06-4993-adb7-21bbe70d3f7c` | ~-Documents-payout-payout-code-workspace | — | — | 33 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-payout-code-workspace/acaed551-937b-4175-8efa-5a019e177f85:acaed551-937b-4175-8efa-5a019e177f85` | ~-Documents-payout-payout-code-workspace | — | — | 60 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-payout-code-workspace/b1092e82-3ce1-4d5c-a27d-7d0a7e3de8b5:b1092e82-3ce1-4d5c-a27d-7d0a7e3de8b5` | ~-Documents-payout-payout-code-workspace | — | — | 6 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-payout-payout-code-workspace/d761c114-9a18-4138-bc45-269a8a088a65:d761c114-9a18-4138-bc45-269a8a088a65` | ~-Documents-payout-payout-code-workspace | — | — | 30 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-payout-worktrees-client-se-testsuit/28452aaf-3033-4c41-8bda-8139c5c36c5f:28452aaf-3033-4c41-8bda-8139c5c36c5f` | ~-Documents-payout-worktrees-client-se-testsuit | — | — | 32 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-worktrees-client-se-testsuit/65aec9af-0663-485c-aaa4-34fcccd0bf97:65aec9af-0663-485c-aaa4-34fcccd0bf97` | ~-Documents-payout-worktrees-client-se-testsuit | — | — | 37 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout-worktrees-client-se-testsuit/e50222a1-2e58-457d-9b46-095512036467:e50222a1-2e58-457d-9b46-095512036467` | ~-Documents-payout-worktrees-client-se-testsuit | — | — | 85 | 0 | n | n | n | — | complete | sensitive |
| `cursor:~-Documents-payout/0d46d907-9fcb-4c34-a7af-9b663049c6f3:0d46d907-9fcb-4c34-a7af-9b663049c6f3` | ~-Documents-payout | — | — | 6 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/2f7a3b2d-f727-466c-bdc8-b56f64eba961:2f7a3b2d-f727-466c-bdc8-b56f64eba961` | ~-Documents-payout | — | — | 17 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/32836cc0-7a05-4066-a24b-855a0b0ade99:32836cc0-7a05-4066-a24b-855a0b0ade99` | ~-Documents-payout | — | — | 18 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/388efed9-776e-4091-9cac-59d10ae10a97:388efed9-776e-4091-9cac-59d10ae10a97` | ~-Documents-payout | — | — | 10 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/4c7b501e-6642-4491-b5cf-fd53d316d578:4c7b501e-6642-4491-b5cf-fd53d316d578` | ~-Documents-payout | — | — | 20 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-payout/50bb5035-c637-482e-b12d-805a953604c6:50bb5035-c637-482e-b12d-805a953604c6` | ~-Documents-payout | — | — | 14 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/5da3651d-951f-49e5-ac83-f4014b541f4f:5da3651d-951f-49e5-ac83-f4014b541f4f` | ~-Documents-payout | — | — | 434 | 7 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/63cff603-1583-40fa-b0f9-d9072df6df7a:63cff603-1583-40fa-b0f9-d9072df6df7a` | ~-Documents-payout | — | — | 49 | 0 | n | n | n | — | complete | sensitive |
| `cursor:~-Documents-payout/9948dc94-b1bc-41de-9d05-c3212f23de8f:9948dc94-b1bc-41de-9d05-c3212f23de8f` | ~-Documents-payout | — | — | 2 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-payout/9ba190ca-7306-4322-bb77-52a106a7c828:9ba190ca-7306-4322-bb77-52a106a7c828` | ~-Documents-payout | — | — | 22 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-payout/9f35e3d3-86af-4342-8aa1-eeb36c1f357d:9f35e3d3-86af-4342-8aa1-eeb36c1f357d` | ~-Documents-payout | — | — | 52 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/a9684182-cb5f-4ca5-8b8b-4c3c84ef682e:a9684182-cb5f-4ca5-8b8b-4c3c84ef682e` | ~-Documents-payout | — | — | 115 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/b07903bd-27db-42d9-be67-f6251bd33eb7:b07903bd-27db-42d9-be67-f6251bd33eb7` | ~-Documents-payout | — | — | 89 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/b67f86c4-02dc-47b6-a24e-5bfaf5a5d9cb:b67f86c4-02dc-47b6-a24e-5bfaf5a5d9cb` | ~-Documents-payout | — | — | 8 | 2 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/bb9afbdf-da56-45a8-a784-6d02f0d4b2e2:bb9afbdf-da56-45a8-a784-6d02f0d4b2e2` | ~-Documents-payout | — | — | 6 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-Documents-payout/c3e4eb25-a4ce-4fc2-893d-02db4b7d3e21:c3e4eb25-a4ce-4fc2-893d-02db4b7d3e21` | ~-Documents-payout | — | — | 13 | 1 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/c5b7734e-812a-4e17-b616-aee0cd23f832:c5b7734e-812a-4e17-b616-aee0cd23f832` | ~-Documents-payout | — | — | 3 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/d6392ce0-0b38-44af-a72d-930c367a1169:d6392ce0-0b38-44af-a72d-930c367a1169` | ~-Documents-payout | — | — | 10 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/d698c640-7ffd-4c29-b069-9f430cf37118:d698c640-7ffd-4c29-b069-9f430cf37118` | ~-Documents-payout | — | — | 24 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/db36ad38-8d3c-4998-897d-a8b61a760efc:db36ad38-8d3c-4998-897d-a8b61a760efc` | ~-Documents-payout | — | — | 64 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/dd6d2e2f-8ac7-4ce0-8b9f-0d64945866dc:dd6d2e2f-8ac7-4ce0-8b9f-0d64945866dc` | ~-Documents-payout | — | — | 49 | 0 | n | n | n | — | complete | internal |
| `cursor:~-Documents-payout/e325dab2-f39d-49cd-b396-10b913ec788f:e325dab2-f39d-49cd-b396-10b913ec788f` | ~-Documents-payout | — | — | 36 | 0 | n | n | n | — | complete | public-ok |
| `cursor:~-codex-worktrees-ba34-dot-agents/689c071c-fcd9-407f-bb15-b7bfea9a539e:689c071c-fcd9-407f-bb15-b7bfea9a539e` | ~-codex-worktrees-ba34-dot-agents | — | — | 13 | 0 | n | n | n | — | complete | internal |

## Known-absent sources
OpenCode: not installed on this machine (checked 2026-07-12: `~/.opencode`, `~/.local/share/opencode`, `~/.config/opencode` all absent). Recorded as known-absent, not a gap.

## Exclusions applied
- Cursor transcripts carry NO per-record timestamps; `started_at`/`ended_at` are unavailable (file mtime is forbidden by the rubric) — reported as `—` for all 158 sessions. This is a source limitation, logged in gaps.
- Cursor records no token/cost/model/wallclock data → those axes are `n`/empty for every session.
- 44 subagent transcripts under `<uuid>/subagents/**` folded into 16 parent sessions.
- Project-dir sidecars excluded (not transcripts): `mcp-approvals.json`, `worker.log`, `terminals/`, `agent-tools/`, `rules/`, `assets/`, `canvases/`, `.DS_Store`.
