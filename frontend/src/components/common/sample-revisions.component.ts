import { CommonModule } from '@angular/common';
import { ChangeDetectorRef, Component, EventEmitter, Input, OnInit, Output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatInputModule } from '@angular/material/input';
import { authState } from '../../hooks/use-auth';
import type { DomainRecord, SampleRevision } from '../../types/domain';
import { formatDate } from '../../utils/format';

interface FieldDiff { label: string; before: string; after: string }

interface ReviseSampleInput {
  expectedVersion: number;
  name: string;
  description: string;
  facility: string;
  owner: string;
  category: string;
  riskLevel: string;
  metricValue: number;
  metricUnit: string;
  effectiveAt: string;
  evidence: string;
  relatedCode: string;
  reason: string;
}

const REJECTION_KEY = 'sample-revision-rejections';

const DIFF_FIELDS: Array<{ key: keyof SampleRevision; label: string }> = [
  { key: 'name', label: '名称' },
  { key: 'facility', label: '设施区域' },
  { key: 'owner', label: '责任人' },
  { key: 'category', label: '分类' },
  { key: 'riskLevel', label: '风险等级' },
  { key: 'metricValue', label: '指标数值' },
  { key: 'metricUnit', label: '指标单位' },
  { key: 'effectiveAt', label: '生效时间' },
  { key: 'evidence', label: '证据' },
  { key: 'relatedCode', label: '关联编码' },
  { key: 'description', label: '描述' },
];

@Component({
  selector: 'app-sample-revisions',
  standalone: true,
  imports: [CommonModule, FormsModule, MatButtonModule, MatInputModule],
  template: `<section class="sample-revision-panel" aria-label="已核验样本版本修订">
    <header>
      <strong>已核验样本版本修订</strong>
      <span>仅最新修订可作为关联合规判断依据；修订原因缺失、版本号过期或同编码同时提交将整次拒绝</span>
    </header>

    <div class="revision-list" *ngIf="verified.length; else empty">
      <article *ngFor="let item of verified" class="revision-card">
        <div class="revision-head">
          <div>
            <strong>{{ item.code }}</strong>
            <small>{{ item.name }} · {{ item.facility }}</small>
          </div>
          <div class="revision-meta">
            <span class="revision-current">当前修订号 v{{ item.version }}</span>
            <button *ngIf="canWrite()" class="table-action" (click)="openRevise(item)">发起修订</button>
          </div>
        </div>

        <div *ngIf="rejectionFor(item.code)" class="alert revision-alert" role="alert">
          修订被拒绝：{{ rejectionFor(item.code) }}（未新增修订，原值保留）
        </div>

        <ng-container *ngIf="sampleRevisions(item) as revisions">
          <div class="revision-timeline" *ngIf="revisions.length; else noHistory">
            <div *ngFor="let revision of revisions; let i = index" class="revision-row" [class.is-current]="revision.current">
              <div class="revision-tag">
                <span class="version-pill" [class.baseline]="revision.kind === 'baseline'">v{{ revision.version }}</span>
                <em class="kind">{{ revision.kind === 'baseline' ? '基线快照' : '修订' }}</em>
                <span class="current-flag" *ngIf="revision.current">当前依据</span>
              </div>
              <div class="revision-body">
                <p class="revision-reason">{{ revision.reason }}</p>
                <small>{{ revision.actor }} · {{ revision.requestId }} · {{ formatDate(revision.createdAt) }}</small>
                <ng-container *ngIf="diffsBetween(revisions[i - 1], revision) as diffs">
                  <ul class="diff-list" *ngIf="diffs.length">
                    <li *ngFor="let diff of diffs">
                      <span class="diff-label">{{ diff.label }}</span>
                      <span class="diff-before">{{ diff.before || '（空）' }}</span>
                      <span class="diff-arrow">→</span>
                      <span class="diff-after">{{ diff.after || '（空）' }}</span>
                    </li>
                  </ul>
                </ng-container>
              </div>
            </div>
          </div>
          <ng-template #noHistory><small class="muted">首次修订时将自动保留原值基线</small></ng-template>
        </ng-container>
      </article>
    </div>
    <ng-template #empty><div class="empty">暂无已核验样本</div></ng-template>

    <div *ngIf="formItem" class="modal-backdrop" (click)="closeRevise()">
      <section class="modal revision-modal" role="dialog" (click)="$event.stopPropagation()">
        <h2>修订已核验样本 · {{ formItem.code }}</h2>
        <p class="modal-hint">当前修订号 <strong>v{{ formItem.version }}</strong>，提交后追加 v{{ formItem.version + 1 }}。请填写修订原因。</p>
        <div *ngIf="formError" class="alert" role="alert">{{ formError }}</div>
        <div class="revision-form">
          <label>修订原因 <em>*</em>
            <textarea rows="2" [(ngModel)]="form.reason" placeholder="例如：分析仪校准偏差修正，需重新关联合规判断"></textarea>
          </label>
          <div class="form-grid">
            <label>名称<input [(ngModel)]="form.name" /></label>
            <label>设施区域<input [(ngModel)]="form.facility" /></label>
            <label>责任人<input [(ngModel)]="form.owner" /></label>
            <label>分类<input [(ngModel)]="form.category" /></label>
            <label>风险等级
              <select [(ngModel)]="form.riskLevel">
                <option value="low">low</option><option value="medium">medium</option>
                <option value="high">high</option><option value="critical">critical</option>
              </select>
            </label>
            <label>指标数值<input type="number" [(ngModel)]="form.metricValue" /></label>
            <label>指标单位<input [(ngModel)]="form.metricUnit" /></label>
            <label>生效时间<input type="datetime-local" [(ngModel)]="form.effectiveAt" /></label>
            <label>关联编码<input [(ngModel)]="form.relatedCode" /></label>
            <label class="span-2">证据<input [(ngModel)]="form.evidence" /></label>
            <label class="span-2">描述<input [(ngModel)]="form.description" /></label>
          </div>
        </div>
        <footer>
          <button mat-button (click)="closeRevise()" [disabled]="submitting">取消</button>
          <button mat-flat-button color="primary" (click)="submit()" [disabled]="submitting">
            {{ submitting ? '提交中…' : '确认追加修订' }}
          </button>
        </footer>
      </section>
    </div>
  </section>`,
})
export class SampleRevisionsComponent implements OnInit {
  @Input() records: DomainRecord[] = [];
  @Output() revised = new EventEmitter<void>();

  readonly formatDate = formatDate;
  formItem: DomainRecord | null = null;
  submitting = false;
  formError = '';
  private rejections: Record<string, string> = {};
  form: ReviseSampleInput = this.emptyForm();

  constructor(private readonly changeDetector: ChangeDetectorRef) {}

  ngOnInit(): void { this.rejections = this.loadRejections(); }

  get verified(): DomainRecord[] {
    return this.records.filter((item) => item.status === 'verified');
  }

  canWrite(): boolean { return authState.hasMinimumRole('operator'); }

  sampleRevisions(item: DomainRecord): SampleRevision[] {
    return (item.revisions || []) as SampleRevision[];
  }

  openRevise(item: DomainRecord): void {
    this.formItem = item;
    this.formError = '';
    this.form = {
      expectedVersion: item.version,
      name: item.name || '', description: item.description || '',
      facility: item.facility || '', owner: item.owner || '', category: item.category || '',
      riskLevel: item.riskLevel || 'medium', metricValue: item.metricValue ?? 0, metricUnit: item.metricUnit || '',
      effectiveAt: this.toLocalInput(item.effectiveAt), evidence: item.evidence || '',
      relatedCode: item.relatedCode || '', reason: '',
    };
    this.changeDetector.detectChanges();
  }

  closeRevise(): void {
    if (this.submitting) return;
    this.formItem = null;
    this.formError = '';
    this.changeDetector.detectChanges();
  }

  async submit(): Promise<void> {
    if (!this.formItem || this.submitting) return;
    if (!this.form.reason.trim()) {
      this.formError = '修订原因不能为空，否则整次请求将被拒绝。';
      this.changeDetector.detectChanges();
      return;
    }
    this.submitting = true;
    this.formError = '';
    try {
      const payload: ReviseSampleInput = {
        ...this.form,
        reason: this.form.reason.trim(),
        expectedVersion: this.formItem.version,
        effectiveAt: new Date(this.form.effectiveAt).toISOString(),
      };
      const response = await fetch(`/api/samples/${this.formItem.id}/revisions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json', Authorization: `Bearer ${this.token()}` },
        body: JSON.stringify(payload),
      });
      const body = await response.json().catch(() => ({ error: 'invalid_response', message: '服务返回了无法解析的响应' }));
      if (!response.ok) {
        this.formError = body.message || body.error || `HTTP ${response.status}`;
        this.recordRejection(this.formItem.code, this.formError);
        return;
      }
      this.clearRejection(this.formItem.code);
      this.formItem = null;
      this.revised.emit();
    } catch (error) {
      this.formError = error instanceof Error ? error.message : String(error);
    } finally {
      this.submitting = false;
      this.changeDetector.detectChanges();
    }
  }

  diffsBetween(before: SampleRevision | undefined, after: SampleRevision): FieldDiff[] {
    if (!before) return [];
    const diffs: FieldDiff[] = [];
    for (const field of DIFF_FIELDS) {
      const oldValue = this.render(field.key, before);
      const newValue = this.render(field.key, after);
      if (oldValue !== newValue) diffs.push({ label: field.label, before: oldValue, after: newValue });
    }
    return diffs;
  }

  private render(key: keyof SampleRevision, revision: SampleRevision): string {
    const value = revision[key];
    if (key === 'effectiveAt') return formatDate(String(value));
    if (key === 'metricValue') return String(Number(value ?? 0));
    return String(value ?? '');
  }

  private toLocalInput(iso: string): string {
    if (!iso) return '';
    const date = new Date(iso);
    if (Number.isNaN(date.getTime())) return '';
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
  }

  private token(): string {
    try { return JSON.parse(localStorage.getItem('domain-control-session') || '{}').token || ''; } catch { return ''; }
  }

  rejectionFor(code: string): string {
    return this.rejections[code] || '';
  }

  private loadRejections(): Record<string, string> {
    try { return JSON.parse(localStorage.getItem(REJECTION_KEY) || '{}'); } catch { return {}; }
  }

  private recordRejection(code: string, reason: string): void {
    this.rejections = { ...this.rejections, [code]: reason };
    localStorage.setItem(REJECTION_KEY, JSON.stringify(this.rejections));
  }

  private clearRejection(code: string): void {
    if (!this.rejections[code]) return;
    delete this.rejections[code];
    this.rejections = { ...this.rejections };
    localStorage.setItem(REJECTION_KEY, JSON.stringify(this.rejections));
  }

  private emptyForm(): ReviseSampleInput {
    return {
      expectedVersion: 1, name: '', description: '', facility: '', owner: '', category: '',
      riskLevel: 'medium', metricValue: 0, metricUnit: '', effectiveAt: '', evidence: '',
      relatedCode: '', reason: '',
    };
  }
}
