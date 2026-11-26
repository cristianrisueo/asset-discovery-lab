# development/

These are the artifacts of the build phase. They are frozen: they record how v0.1.0 was specified, planned, built and verified, and they are not updated any more.

| Path | What |
|---|---|
| `system-spec.md` | The system specification the base was built from |
| `sprints-plan.md` | The build order: six sprints, 0 and A to E |
| `decision-log.md` | Every rule applied, fallback taken and version pinned during the build (D-001 to D-050) |
| `open-questions.md` | Doubts raised during the build (Q-001 to Q-003) |
| `build-progress.md` | The memory the unattended build kept between sessions |
| `sprints/` | The plan, build report and audit report of each sprint |
| `verification-report.md` | The owner-attended verification of v0.1.0 against the specification |
| `run-sprints.sh` | The script that ran the unattended build, one session per sprint |
| `CLAUDE.build.md` | The `CLAUDE.md` that every build session loaded. Renamed so that its build-phase rules do not load in future sessions |

**Paths inside these documents use their original locations.** `specs/…` means this folder, and `CLAUDE.md` and `run-sprints.sh` refer to the repository root, where those files lived during the build. The documents are kept exactly as they were written.
