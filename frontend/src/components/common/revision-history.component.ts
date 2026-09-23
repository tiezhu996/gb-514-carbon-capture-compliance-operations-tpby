import { CommonModule } from '@angular/common';
import { Component, Input } from '@angular/core';
import type { DomainRecord, SampleRevision } from '../../types/domain';
import { formatDate } from '../../utils/format';

@Component({
  selector: 'app-revision-history',
  standalone: true,
  imports: [CommonModule],
  template: `<section class="revision-panel" aria-label="样本修订历史">
    <header><strong>样本修订历史</strong><span>仅最新修订作为关联合规判断依据</span></header>
    <div *ngIf="records.length; else empty" class="revision-grid">
      <article *ngFor="let item of records.slice(0, 4)">
        <header><strong>{{ item.code }}</strong><span class="revision-current">当前修订 v{{ item.version }}</span></header>
        <ol class="revision-list" *ngIf="revisionsOf(item).length; else noRevision">
          <li *ngFor="let revision of latestRevisions(item)">
            <div><strong>v{{ revision.version }}</strong><span>{{ revision.state }} · {{ revision.metricValue }} {{ revision.metricUnit }} · {{ revision.riskLevel }}</span></div>
            <em>{{ deltaText(item, revision) }}</em>
            <small>{{ revision.reason }} · {{ revision.actor }} · {{ revision.requestId }}</small>
          </li>
        </ol>
        <ng-template #noRevision><p class="muted">暂无修订快照</p></ng-template>
        <div *ngIf="item.rejections?.length" class="rejection-list">
          <p *ngFor="let rejection of item.rejections!.slice(0, 3)">
            已拒绝：{{ rejection.reason }}<small>{{ rejection.actor }} · {{ formatDate(rejection.createdAt) }}</small>
          </p>
        </div>
      </article>
    </div>
    <ng-template #empty><div class="empty">暂无修订记录</div></ng-template>
  </section>`,
})
export class RevisionHistoryComponent {
  @Input() records: DomainRecord[] = [];
  readonly formatDate = formatDate;

  revisionsOf(item: DomainRecord): SampleRevision[] {
    return (item.revisions || []).filter((revision): revision is SampleRevision => 'emissionSampleId' in revision);
  }
  latestRevisions(item: DomainRecord): SampleRevision[] {
    return this.revisionsOf(item).slice(-5).reverse();
  }
  deltaText(item: DomainRecord, revision: SampleRevision): string {
    const revisions = this.revisionsOf(item);
    const index = revisions.indexOf(revision);
    if (index <= 0) return '基线修订';
    const previous = revisions[index - 1];
    const parts: string[] = [];
    const delta = revision.metricValue - previous.metricValue;
    if (delta !== 0) parts.push(`指标 ${delta > 0 ? '+' : ''}${delta.toFixed(1)} ${revision.metricUnit}`);
    if (revision.state !== previous.state) parts.push(`状态 ${previous.state}→${revision.state}`);
    if (revision.riskLevel !== previous.riskLevel) parts.push(`风险 ${previous.riskLevel}→${revision.riskLevel}`);
    return parts.length ? parts.join('；') : '无字段差异';
  }
}
