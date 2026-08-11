import React, { FormEvent, useEffect, useState } from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import type { RegistryDescriptor, RegistryGrant } from "../types";

export function ArtifactsPage(props: { pathname: string }): React.JSX.Element {
  const [repositories, setRepositories] = useState<string[]>([]);
  const [selectedRepository, setSelectedRepository] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [digest, setDigest] = useState("");
  const [referrers, setReferrers] = useState<RegistryDescriptor[]>([]);
  const [grants, setGrants] = useState<RegistryGrant[]>([]);
  const [subjectType, setSubjectType] = useState("user");
  const [subjectId, setSubjectID] = useState("admin");
  const [pathPrefix, setPathPrefix] = useState("appliance/");
  const [actions, setActions] = useState({ pull: true, push: false });
  const [message, setMessage] = useState("");

  useEffect(() => {
    void (async () => {
      const nextRepositories = await client.listRepositories();
      setRepositories(nextRepositories);
      setSelectedRepository(nextRepositories[0] || "");
      setGrants(await client.listRegistryGrants());
    })();
  }, []);

  useEffect(() => {
    if (!selectedRepository) {
      return;
    }
    void client.listRepositoryTags(selectedRepository).then(setTags);
  }, [selectedRepository]);

  async function loadReferrers() {
    setReferrers(await client.listRepositoryReferrers(selectedRepository, digest));
  }

  async function createGrant(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await client.createRegistryGrant({
      subjectType,
      subjectId,
      pathPrefix,
      actions: Object.entries(actions)
        .filter((entry) => entry[1])
        .map((entry) => entry[0])
    });
    setMessage("Registry grant created.");
    setGrants(await client.listRegistryGrants());
  }

  async function deleteGrant(id: string) {
    await client.deleteRegistryGrant(id);
    setMessage("Registry grant deleted.");
    setGrants(await client.listRegistryGrants());
  }

  return (
    <PageFrame
      title="Artifacts"
      eyebrow=""
      description="Registry catalog visibility, grants, and client access credentials."
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Catalog", path: "/manage/artifacts" },
        { label: "Grants", path: "/manage/artifacts/grants" }
      ]}
    >
      <div className="stack">
        {message ? <div className="message">{message}</div> : null}
        <Card
          title="Client API tokens"
          subtitle="Registry login uses your username plus an API token from Account → API Keys"
        >
          <p className="m-0 text-sm leading-6 text-slate-600">
            Create or revoke artifact-server API tokens under Account → API Keys (user menu → Manage API keys).
            Grants below only authorize repository path prefixes after that token authenticates.
          </p>
          <div className="button-row mt-3">
            <button className="button button--primary" type="button" onClick={() => navigate("/account/api-keys")}>
              Manage API keys
            </button>
          </div>
        </Card>
        {props.pathname === "/manage/artifacts/grants" ? (
          <div className="grid-two">
            <Card title="Create registry grant" subtitle="User or role-scoped grant creation">
              <form className="stack-form" onSubmit={createGrant}>
                <label className="field">
                  <span>Subject type</span>
                  <select value={subjectType} onChange={(event) => setSubjectType(event.target.value)}>
                    <option value="user">User</option>
                    <option value="role">Role</option>
                  </select>
                </label>
                <label className="field">
                  <span>Subject ID</span>
                  <input value={subjectId} onChange={(event) => setSubjectID(event.target.value)} />
                </label>
                <label className="field">
                  <span>Path prefix</span>
                  <input value={pathPrefix} onChange={(event) => setPathPrefix(event.target.value)} />
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={actions.pull}
                    onChange={(event) => setActions((current) => ({ ...current, pull: event.target.checked }))}
                  />
                  <span>Pull</span>
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={actions.push}
                    onChange={(event) => setActions((current) => ({ ...current, push: event.target.checked }))}
                  />
                  <span>Push</span>
                </label>
                <button className="button button--primary" type="submit">
                  Create grant
                </button>
              </form>
            </Card>
            <Card title="Current grants" subtitle="Existing registry grant entries">
              {grants.length === 0 ? (
                <EmptyState message="No registry grants yet." />
              ) : (
                <div className="table-list">
                  {grants.map((grant) => (
                    <div className="table-list__row" key={grant.id}>
                      <div>
                        <strong>
                          {grant.subjectType}:{grant.subjectId}
                        </strong>
                        <span>
                          {grant.pathPrefix} · {grant.actions.join(", ")}
                        </span>
                      </div>
                      <button className="button button--ghost" onClick={() => void deleteGrant(grant.id)}>
                        Delete
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          </div>
        ) : (
          <div className="grid-two">
            <Card title="Repository catalog" subtitle="Browse available repositories and tags">
              <label className="field">
                <span>Repository</span>
                <select
                  value={selectedRepository}
                  onChange={(event) => setSelectedRepository(event.target.value)}
                >
                  {repositories.map((repository) => (
                    <option key={repository} value={repository}>
                      {repository}
                    </option>
                  ))}
                </select>
              </label>
              <div className="badge-row">
                {tags.map((tag) => (
                  <span className="pill" key={tag}>
                    {tag}
                  </span>
                ))}
              </div>
            </Card>
            <Card title="Artifact referrers" subtitle="Digest-based referrer lookup">
              <label className="field">
                <span>Digest</span>
                <input value={digest} onChange={(event) => setDigest(event.target.value)} />
              </label>
              <button className="button button--primary" onClick={() => void loadReferrers()}>
                Load referrers
              </button>
              <div className="table-list">
                {referrers.map((referrer) => (
                  <div className="table-list__row" key={referrer.digest}>
                    <div>
                      <strong>{referrer.artifactType || referrer.mediaType}</strong>
                      <span>{referrer.digest}</span>
                    </div>
                    <span>{referrer.size} bytes</span>
                  </div>
                ))}
              </div>
            </Card>
          </div>
        )}
      </div>
    </PageFrame>
  );
}
