// Keeps one comment on each pull request saying which container image holds its changes, for actions/github-script: require it from a checkout of the default branch and await it with the script's github and context.
const marker = '<!-- encore-image -->';

module.exports = async ({ github, context }, { tag, prs, digest, sha, runUrl }) => {
  const image = `ghcr.io/${context.repo.owner}/${context.repo.repo}`.toLowerCase();
  const commit = [sha && sha.slice(0, 7), runUrl && `([build](${runUrl}))`].filter(Boolean).join(' ');
  const built = [commit, digest && `\`sha256:${digest}\``].filter(Boolean);
  const removed = tag.startsWith('PR') ? 'the pull request closes' : 'the branch is deleted';
  const body = [
    marker,
    '### Container image',
    '',
    'Built from this pull request for linux/amd64, linux/arm64 and linux/arm/v7, and updated on every push:',
    '',
    '```sh',
    `docker pull ${image}:${tag}`,
    '```',
    '',
    ...(built.length > 0 ? [`Latest: ${built.join(', ')}.`, ''] : []),
    `It is removed when ${removed}.`,
  ].join('\n');

  for (const number of prs) {
    const issue = { ...context.repo, issue_number: number };
    const comments = await github.paginate(github.rest.issues.listComments, issue);
    const mine = comments.find((c) => c.user.type === 'Bot' && c.body.startsWith(marker));

    if (mine) {
      await github.rest.issues.updateComment({ ...context.repo, comment_id: mine.id, body });
    } else {
      await github.rest.issues.createComment({ ...issue, body });
    }
  }
};
